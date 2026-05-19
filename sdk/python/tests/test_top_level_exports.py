"""Tests for top-level exports in omneval_sdk.__init__.py."""
from unittest import mock

from omneval_sdk.trace import trace

# OTel imports used by test helpers.
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter as _OTelInMemoryExporter,
)
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

# All functions re-exported from omneval_sdk.trace at the package level.
_EXPORTED_FUNCTIONS = [
    "set_input",
    "set_output",
    "set_model",
    "set_tokens",
    "get_active_span",
]


class TestTopLevelExports:
    """Verify key helper functions are accessible from the top-level omneval_sdk package."""

    def _make_test_provider(self):
        """Create a TracerProvider with an in-memory exporter for testing."""
        exporter = _OTelInMemoryExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))
        return provider, exporter

    def test_exported_functions_are_accessible_and_callable(self):
        """All functions from omneval_sdk.trace are accessible and callable."""
        import omneval_sdk

        for name in _EXPORTED_FUNCTIONS:
            assert hasattr(omneval_sdk, name), f"{name} not exported at package level"
            func = getattr(omneval_sdk, name)
            assert callable(func), f"{name} is not callable"

    def test_get_active_span_returns_none_outside_decorator(self):
        """get_active_span returns None when called outside a decorated function."""
        import omneval_sdk

        assert omneval_sdk.get_active_span() is None

    def test_usage_pattern_works_without_internal_imports(self):
        """The acceptance-criterion usage pattern works without internal imports."""
        import omneval_sdk

        provider, exporter = self._make_test_provider()

        old_provider = omneval_sdk.exporter._tracer_provider
        try:
            with mock.patch(
                "omneval_sdk.trace.get_tracer_provider", return_value=provider
            ):

                @omneval_sdk.trace
                def call_llm(prompt):
                    span = omneval_sdk.get_active_span()
                    omneval_sdk.set_model(span, "gpt-4o")
                    omneval_sdk.set_input(span, prompt)
                    omneval_sdk.set_output(span, "Hello!")
                    return "done"

                result = call_llm("hello")
                assert result == "done"

                spans = exporter.get_finished_spans()
                assert len(spans) >= 1

                span = spans[0]
                assert span.attributes.get("gen_ai.request.model") == "gpt-4o"
                assert span.attributes.get("omneval.input") == "hello"
                assert span.attributes.get("omneval.output") == "Hello!"
        finally:
            omneval_sdk.exporter._tracer_provider = old_provider

    def test_set_tokens_sets_span_attributes(self):
        """set_tokens attaches input and output token counts to a span."""
        import omneval_sdk

        provider, exporter = self._make_test_provider()

        old_provider = omneval_sdk.exporter._tracer_provider
        try:
            with mock.patch(
                "omneval_sdk.trace.get_tracer_provider", return_value=provider
            ):

                @omneval_sdk.trace
                def fn_with_tokens():
                    span = omneval_sdk.get_active_span()
                    omneval_sdk.set_tokens(span, 100, 50)
                    return "ok"

                fn_with_tokens()

                spans = exporter.get_finished_spans()
                assert len(spans) >= 1
                span = spans[0]
                assert span.attributes.get("gen_ai.usage.input_tokens") == 100
                assert span.attributes.get("gen_ai.usage.output_tokens") == 50
        finally:
            omneval_sdk.exporter._tracer_provider = old_provider
