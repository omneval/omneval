"""Tests for top-level exports in lantern_sdk.__init__.py."""
import inspect


class TestTopLevelExports:
    """Verify key helper functions are accessible from the top-level lantern_sdk package."""

    def test_set_input_is_exported(self):
        """set_input is accessible as lantern_sdk.set_input."""
        import lantern_sdk
        assert hasattr(lantern_sdk, "set_input")
        assert callable(lantern_sdk.set_input)

    def test_set_output_is_exported(self):
        """set_output is accessible as lantern_sdk.set_output."""
        import lantern_sdk
        assert hasattr(lantern_sdk, "set_output")
        assert callable(lantern_sdk.set_output)

    def test_set_model_is_exported(self):
        """set_model is accessible as lantern_sdk.set_model."""
        import lantern_sdk
        assert hasattr(lantern_sdk, "set_model")
        assert callable(lantern_sdk.set_model)

    def test_set_tokens_is_exported(self):
        """set_tokens is accessible as lantern_sdk.set_tokens."""
        import lantern_sdk
        assert hasattr(lantern_sdk, "set_tokens")
        assert callable(lantern_sdk.set_tokens)

    def test_get_active_span_is_exported(self):
        """get_active_span is accessible as lantern_sdk.get_active_span."""
        import lantern_sdk
        assert hasattr(lantern_sdk, "get_active_span")
        assert callable(lantern_sdk.get_active_span)

    def test_all_exports_are_callable(self):
        """All symbols in __all__ that are functions should be callable."""
        import lantern_sdk
        for name in ["set_input", "set_output", "set_model", "set_tokens", "get_active_span"]:
            assert name in dir(lantern_sdk), f"{name} not in dir(lantern_sdk)"

    def test_usage_pattern_without_internal_imports(self):
        """The acceptance-criterion usage pattern works without internal imports."""
        import lantern_sdk
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor
        from unittest import mock

        # Set up a test provider
        exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))

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

                # Verify the span has the expected attributes.
                span = spans[0]
                assert span.attributes.get("gen_ai.request.model") == "gpt-4o"
                assert span.attributes.get("lantern.input") == "hello"
                assert span.attributes.get("lantern.output") == "Hello!"
        finally:
            lantern_sdk.exporter._tracer_provider = old_provider

    def test_set_tokens_usage(self):
        """set_tokens sets input and output token attributes on a span."""
        import lantern_sdk
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor
        from unittest import mock

        exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))

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

    def test_get_active_span_returns_none_when_no_decorator(self):
        """get_active_span returns None outside a decorated function."""
        import lantern_sdk
        result = lantern_sdk.get_active_span()
        assert result is None
