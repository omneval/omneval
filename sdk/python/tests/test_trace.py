"""Tests for @lantern.trace decorator and configure function."""
import responses
from unittest import mock

import lantern_sdk
from lantern_sdk.trace import trace

# OTel imports used by test helpers.
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter as _OTelInMemoryExporter,
)
from opentelemetry.sdk.trace.export import SimpleSpanProcessor


class TestConfigure:
    """Tests for lantern.configure(endpoint, api_key)."""

    def teardown_method(self, method) -> None:
        """Restore tracer provider after each test to avoid state leakage."""
        lantern_sdk.exporter._tracer_provider = None

    @responses.activate
    def test_configure_wires_otlp_exporter(self):
        """configure() sets up an OTLP HTTP exporter pointing at the endpoint."""
        responses.add(
            responses.POST,
            "http://localhost:4318/v1/traces",
            body="ok",
            status=200,
        )

        lantern_sdk.configure("http://localhost:4318", "ltn_proj_testkey")

        @trace
        def dummy():
            return "ok"

        result = dummy()
        assert result == "ok"

    @responses.activate
    def test_configure_with_https(self):
        """configure() works with https:// endpoints."""
        responses.add(
            responses.POST,
            "https://lantern.example.com/v1/traces",
            body="ok",
            status=200,
        )

        lantern_sdk.configure("https://lantern.example.com", "ltn_proj_key")

        @trace
        def dummy():
            return "ok"

        dummy()


class TestTraceDecorator:
    """Tests for @lantern.trace decorator."""

    def _make_test_provider(self):
        """Create a TracerProvider with an in-memory exporter for testing."""
        exporter = _OTelInMemoryExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))
        return provider, exporter

    def test_decorator_creates_span(self):
        """@lantern.trace on a function creates a span exported to OTLP endpoint."""
        provider, exporter = self._make_test_provider()
        old_provider = lantern_sdk.exporter._tracer_provider

        with mock.patch(
            "lantern_sdk.trace.get_tracer_provider", return_value=provider
        ):

            @trace
            def my_function(arg1, arg2="default"):
                return f"{arg1}-{arg2}"

            result = my_function("hello")
            assert result == "hello-default"

            spans = exporter.get_finished_spans()
            assert len(spans) >= 1
            assert spans[0].name == "my_function"

        lantern_sdk.exporter._tracer_provider = old_provider

    def test_decorator_function_name_in_span(self):
        """Span is created with the function name as the span name."""
        provider, exporter = self._make_test_provider()
        old_provider = lantern_sdk.exporter._tracer_provider

        with mock.patch(
            "lantern_sdk.trace.get_tracer_provider", return_value=provider
        ):

            @trace
            def named_function():
                pass

            named_function()

            spans = exporter.get_finished_spans()
            assert len(spans) >= 1
            assert spans[0].name == "named_function"

        lantern_sdk.exporter._tracer_provider = old_provider

    def test_nested_decorated_functions_produce_linked_spans(self):
        """Nested decorated functions produce a correctly linked parent-child span tree."""
        provider, exporter = self._make_test_provider()
        old_provider = lantern_sdk.exporter._tracer_provider

        with mock.patch(
            "lantern_sdk.trace.get_tracer_provider", return_value=provider
        ):

            @trace
            def outer():
                return inner()

            @trace
            def inner():
                return "nested result"

            outer()

            spans = exporter.get_finished_spans()
            names = [s.name for s in spans]
            assert "outer" in names
            assert "inner" in names

        lantern_sdk.exporter._tracer_provider = old_provider

    def test_span_has_context_propagation(self):
        """Child spans reference parent span via parent_span_id."""
        provider, exporter = self._make_test_provider()
        old_provider = lantern_sdk.exporter._tracer_provider

        with mock.patch(
            "lantern_sdk.trace.get_tracer_provider", return_value=provider
        ):

            @trace
            def parent():
                return child()

            @trace
            def child():
                return "done"

            parent()

            spans = exporter.get_finished_spans()

            child_span = next((s for s in spans if s.name == "child"), None)
            parent_span = next((s for s in spans if s.name == "parent"), None)

            assert child_span is not None, "child span not found"
            assert parent_span is not None, "parent span not found"

            # Child span should reference parent via parent span_id.
            assert child_span.parent is not None, "child span should have a parent"
            assert child_span.parent.span_id == parent_span.context.span_id

    def test_decorator_propagates_exceptions(self):
        """@lantern.trace propagates exceptions raised by the decorated function."""
        provider, exporter = self._make_test_provider()
        old_provider = lantern_sdk.exporter._tracer_provider

        with mock.patch(
            "lantern_sdk.trace.get_tracer_provider", return_value=provider
        ):

            @trace
            def failing_function():
                raise RuntimeError("oops")

            try:
                failing_function()
                assert False, "Expected RuntimeError"
            except RuntimeError as e:
                assert str(e) == "oops"

            # Even on exception, the span should be recorded.
            spans = exporter.get_finished_spans()
            assert len(spans) >= 1
            # Check the span status is set to ERROR.
            assert spans[0].status.status_code.name == "ERROR"
