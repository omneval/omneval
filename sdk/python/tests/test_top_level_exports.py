"""Tests for top-level exports in lantern_sdk.__init__.py."""
from unittest import mock

from lantern_sdk.trace import trace

# OTel imports used by test helpers.
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter as _OTelInMemoryExporter,
)
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

# All functions re-exported from lantern_sdk.trace at the package level.
_EXPORTED_FUNCTIONS = [
    "set_input",
    "set_output",
    "set_model",
    "set_tokens",
    "get_active_span",
]


class TestTopLevelExports:
    """Verify key helper functions are accessible from the top-level lantern_sdk package."""

    def _make_test_provider(self):
        """Create a TracerProvider with an in-memory exporter for testing."""
        exporter = _OTelInMemoryExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))
        return provider, exporter

    def test_exported_functions_are_accessible_and_callable(self):
        """All functions from lantern_sdk.trace are accessible and callable."""
        import lantern_sdk

        for name in _EXPORTED_FUNCTIONS:
            assert hasattr(lantern_sdk, name), f"{name} not exported at package level"
            func = getattr(lantern_sdk, name)
            assert callable(func), f"{name} is not callable"

    def test_get_active_span_returns_none_outside_decorator(self):
        """get_active_span returns None when called outside a decorated function."""
        import lantern_sdk

        assert lantern_sdk.get_active_span() is None

    def test_usage_pattern_works_without_internal_imports(self):
        """The acceptance-criterion usage pattern works without internal imports."""
        import lantern_sdk

        provider, exporter = self._make_test_provider()

        old_provider = lantern_sdk.exporter._tracer_provider
        try:
            with mock.patch(
                "lantern_sdk.trace.get_tracer_provider", return_value=provider
            ):

                @lantern_sdk.trace
                def call_llm(prompt):
                    span = lantern_sdk.get_active_span()
                    lantern_sdk.set_model(span, "gpt-4o")
                    lantern_sdk.set_input(span, prompt)
                    lantern_sdk.set_output(span, "Hello!")
                    return "done"

                result = call_llm("hello")
                assert result == "done"

                spans = exporter.get_finished_spans()
                assert len(spans) >= 1

                span = spans[0]
                assert span.attributes.get("gen_ai.request.model") == "gpt-4o"
                assert span.attributes.get("lantern.input") == "hello"
                assert span.attributes.get("lantern.output") == "Hello!"
        finally:
            lantern_sdk.exporter._tracer_provider = old_provider

    def test_set_tokens_sets_span_attributes(self):
        """set_tokens attaches input and output token counts to a span."""
        import lantern_sdk

        provider, exporter = self._make_test_provider()

        old_provider = lantern_sdk.exporter._tracer_provider
        try:
            with mock.patch(
                "lantern_sdk.trace.get_tracer_provider", return_value=provider
            ):

                @lantern_sdk.trace
                def fn_with_tokens():
                    span = lantern_sdk.get_active_span()
                    lantern_sdk.set_tokens(span, 100, 50)
                    return "ok"

                fn_with_tokens()

                spans = exporter.get_finished_spans()
                assert len(spans) >= 1
                span = spans[0]
                assert span.attributes.get("gen_ai.usage.input_tokens") == 100
                assert span.attributes.get("gen_ai.usage.output_tokens") == 50
        finally:
            lantern_sdk.exporter._tracer_provider = old_provider
