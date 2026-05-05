"""OTLP HTTP exporter configuration for Lantern."""
from __future__ import annotations

from typing import Optional

from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.semconv.resource import ResourceAttributes

# Global tracer provider managed by the SDK.
_tracer_provider: Optional[TracerProvider] = None


def configure(endpoint: str, api_key: str) -> None:
    """Configure an OTLP HTTP exporter pointing at the Lantern ingest endpoint.

    Must be called once at application startup before any spans are started.

    Args:
        endpoint: Base URL of the Lantern ingest API (e.g. http://localhost:4318).
        api_key: API key for authentication (sent as X-API-Key header).
    """
    global _tracer_provider

    # Stop existing provider if any.
    if _tracer_provider is not None:
        _tracer_provider.shutdown()

    # Build the OTLP endpoint URL. The exporter auto-appends /v1/traces.
    if not endpoint.endswith("/"):
        endpoint += "/"

    exporter = OTLPSpanExporter(
        endpoint=endpoint + "v1/traces",
        headers={"X-API-Key": api_key},
    )

    # Create a new tracer provider with the exporter.
    _tracer_provider = TracerProvider(
        resource=__make_resource(),
    )
    # Use SimpleSpanProcessor for synchronous export (spans exported immediately
    # on span end, in the same thread). This matches the Go SDK's WithSyncer
    # approach and makes testing reliable.
    _tracer_provider.add_span_processor(
        SimpleSpanProcessor(exporter),
    )


def get_tracer_provider() -> Optional[TracerProvider]:
    """Return the current tracer provider, or None if not configured."""
    return _tracer_provider


def __make_resource():
    """Create an OpenTelemetry resource for the Lantern SDK."""
    from opentelemetry.sdk.resources import Resource

    return Resource.create({
        ResourceAttributes.SERVICE_NAME: "lantern-sdk",
    })
