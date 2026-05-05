"""HTTP client for prompt fetch and manual score writes."""
from __future__ import annotations

import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Optional

import requests


@dataclass
class _CacheEntry:
    """A cache entry with an optional expiry time."""
    template: str
    expires_at: Optional[float] = None  # None means no TTL


@dataclass
class _ModelConfig:
    """Model configuration for a prompt version."""
    model: str = ""
    temperature: float = 0.0
    max_tokens: int = 0


@dataclass
class PromptResponse:
    """Response from the prompt API."""
    name: str
    version: int
    template: str
    model_config: dict[str, Any] = field(default_factory=dict)


class LanternClient:
    """HTTP client for prompt fetch and manual score writes.

    Prompt responses are cached client-side: label lookups use a
    30-second TTL; version lookups have no TTL (immutable data).

    Args:
        base_url: Base URL of the Lantern Query API (e.g. http://localhost:8080).
        api_key: API key for authentication (reserved for future use).
    """

    def __init__(self, base_url: str, api_key: str = "") -> None:
        self._base_url = base_url.rstrip("/")
        self._http = requests.Session()
        self._http.timeout = 10.0
        self._label_cache: dict[str, _CacheEntry] = {}
        self._version_cache: dict[str, str] = {}
        self._label_lock = __import__("threading").Lock()
        self._version_lock = __import__("threading").Lock()

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
            httpx.HTTPStatusError: If the server returns an error status.
        """
        if label == "":
            label = "production"

        cache_key = f"{name}|{label}"
        now = time.time()

        # Check label cache first.
        with self._label_lock:
            entry = self._label_cache.get(cache_key)
            if entry is not None and entry.expires_at is not None and now < entry.expires_at:
                return {
                    "name": name,
                    "version": 0,
                    "template": entry.template,
                    "model_config": {},
                }
            if entry is not None and (entry.expires_at is None or now >= entry.expires_at):
                # Expired — clear it so we re-fetch.
                del self._label_cache[cache_key]

        # Cache miss — fetch from server.
        resp = self._fetch_prompt_from_server(name, label=label)

        # Store in label cache with 30-second TTL.
        with self._label_lock:
            self._label_cache[cache_key] = _CacheEntry(
                template=resp["template"],
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
            httpx.HTTPStatusError: If the server returns an error status.
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
            httpx.HTTPStatusError: If the server returns an error status.
        """
        if not span_id:
            raise ValueError("span_id is required")

        trace_id = uuid.uuid4().hex[:32]

        payload = {
            "span_id": span_id,
            "trace_id": trace_id,
            "eval_name": eval_name,
            "value": value,
            "reasoning": reasoning,
        }

        url = f"{self._base_url}/api/v1/scores"
        resp = self._http.post(url, json=payload, timeout=10)
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

        resp = self._http.get(url, timeout=10)
        if resp.status_code == 404:
            raise httpx.HTTPStatusError(
                f"prompt not found: {name}",
                request=resp.request,
                response=resp,
            )
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
