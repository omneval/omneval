"""HTTP client for prompt fetch and manual score writes."""
from __future__ import annotations

import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Optional

import requests


@dataclass
class _CacheEntry:
    """A cache entry with an optional expiry time."""
    template: str
    version: int = 0
    model_config: dict[str, Any] = field(default_factory=dict)
    expires_at: Optional[float] = None  # None means no TTL


class LanternClient:
    """HTTP client for prompt fetch and manual score writes.

    Prompt responses are cached client-side: label lookups use a
    30-second TTL; version lookups have no TTL (immutable data).

    Args:
        base_url: Base URL of the Lantern Query API (e.g. http://localhost:8080).
        api_key: API key for authentication (e.g. ltn_proj_<43 base58>).
                 The project_id is extracted from the API key suffix and
                 automatically included in score write requests.
        project_id: Optional explicit project ID. If provided, overrides the
                    value extracted from the API key.
    """

    def __init__(
        self,
        base_url: str,
        api_key: str = "",
        project_id: Optional[str] = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._http = requests.Session()
        self._label_cache: dict[str, _CacheEntry] = {}
        self._version_cache: dict[str, str] = {}
        self._label_lock = threading.Lock()
        self._version_lock = threading.Lock()
        self._project_id = project_id or self._extract_project_id(api_key)

    @staticmethod
    def _extract_project_id(api_key: str) -> str:
        """Extract the project_id suffix from an API key like ltn_proj_<suffix>."""
        if api_key.startswith("ltn_proj_"):
            return api_key[len("ltn_proj_"):]
        if api_key.startswith("ltn_svc_"):
            return api_key[len("ltn_svc_"):]
        return api_key

    def get_prompt(self, name: str, label: str = "production") -> dict[str, Any]:
        """Fetch a prompt by name and label.

        Returns a dict with 'name', 'version', 'template', and 'model_config'.
        Uses a 30-second TTL cache for label lookups.

        Args:
            name: Prompt name.
            label: Prompt label (default: "production").

        Returns:
            Dict containing prompt data.

        Raises:
            requests.HTTPError: If the server returns an error status.
        """
        if label == "":
            label = "production"

        cache_key = f"{name}|{label}"
        now = time.time()

        # Check label cache before acquiring the lock.
        with self._label_lock:
            entry = self._label_cache.get(cache_key)

        if entry is not None and entry.expires_at is not None and now < entry.expires_at:
            return {
                "name": name,
                "version": entry.version,
                "template": entry.template,
                "model_config": entry.model_config,
            }

        # Evict expired or non-expiring (immutable) entries.
        with self._label_lock:
            if cache_key in self._label_cache:
                del self._label_cache[cache_key]

        # Cache miss — fetch from server.
        resp = self._fetch_prompt_from_server(name, label=label)

        # Store in label cache with 30-second TTL.
        with self._label_lock:
            self._label_cache[cache_key] = _CacheEntry(
                template=resp["template"],
                version=resp["version"],
                model_config=resp["model_config"],
                expires_at=now + 30.0,
            )

        return resp

    def get_prompt_version(self, name: str, version: int) -> dict[str, Any]:
        """Fetch a prompt by name and explicit version number.

        Uses a no-TTL cache (immutable data).

        Args:
            name: Prompt name.
            version: Version number.

        Returns:
            Dict containing prompt data.

        Raises:
            requests.HTTPError: If the server returns an error status.
        """
        cache_key = f"{name}|{version}"

        # Check version cache first.
        with self._version_lock:
            if cache_key in self._version_cache:
                return {
                    "name": name,
                    "version": version,
                    "template": self._version_cache[cache_key],
                    "model_config": {},
                }

        # Cache miss — fetch from server.
        resp = self._fetch_prompt_from_server(name, version=version)

        # Store in version cache (no TTL).
        with self._version_lock:
            self._version_cache[cache_key] = resp["template"]

        return resp

    def write_score(
        self,
        span_id: str,
        eval_name: str,
        value: float,
        reasoning: str = "",
    ) -> None:
        """Submit a manual score for a span.

        Args:
            span_id: Span ID to score.
            eval_name: Name of the evaluation.
            value: Numeric score value.
            reasoning: Optional reasoning for the score.

        Raises:
            ValueError: If span_id is empty.
            requests.HTTPError: If the server returns an error status.
        """
        if not span_id:
            raise ValueError("span_id is required")

        trace_id = uuid.uuid4().hex[:32]

        payload = {
            "span_id": span_id,
            "trace_id": trace_id,
            "project_id": self._project_id,
            "eval_name": eval_name,
            "value": value,
            "reasoning": reasoning,
        }

        url = f"{self._base_url}/api/v1/scores"
        resp = self._http.post(url, json=payload, timeout=10.0)
        resp.raise_for_status()

    def _fetch_prompt_from_server(
        self,
        name: str,
        label: Optional[str] = None,
        version: Optional[int] = None,
    ) -> dict[str, Any]:
        """Fetch a prompt version from the server API."""
        if version is not None and version > 0:
            url = f"{self._base_url}/api/v1/prompts/{name}?version={version}"
        else:
            if label is None or label == "":
                label = "production"
            url = f"{self._base_url}/api/v1/prompts/{name}?label={label}"

        resp = self._http.get(url, timeout=10.0)
        if resp.status_code == 404:
            raise requests.HTTPError(f"prompt not found: {name}")
        resp.raise_for_status()

        data = resp.json()
        return {
            "name": data.get("name", name),
            "version": data.get("version", 0),
            "template": data.get("template", ""),
            "model_config": data.get("model_config", data.get("modelConfig", {})),
        }

    def close(self) -> None:
        """Close the underlying HTTP client."""
        self._http.close()

    def __enter__(self) -> "LanternClient":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()
