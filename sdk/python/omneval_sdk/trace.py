"""@omneval.trace decorator for instrumenting functions."""

from __future__ import annotations

import contextvars
import functools
import uuid
from typing import Callable, Optional, TypeVar, Any

from opentelemetry import context as otel_context
from opentelemetry import trace as otel_trace
from opentelemetry.trace import Span, Status, StatusCode

from omneval_sdk.exporter import get_tracer_provider

F = TypeVar("F", bound=Callable[..., Any])

# Context variable for carrying the active span in nested calls.
_current_span: contextvars.ContextVar[Optional[Span]] = contextvars.ContextVar(
    "_current_span",
    default=None,
)


def trace(fn: F) -> F:
    """Decorator that wraps a function in an OTel span sent to Omneval.

    Creates a span with the function name, propagates context to nested
    decorated calls via contextvars, and exports the span to the configured
    OTLP endpoint.

    Usage::

        @trace
        def my_function(arg1, arg2="default"):
            return f"{arg1}-{arg2}"
    """

    @functools.wraps(fn)
    def wrapper(*args: Any, **kwargs: Any) -> Any:
        provider = get_tracer_provider()
        if provider is None:
            # Not configured — call function directly.
            return fn(*args, **kwargs)

        tracer = provider.get_tracer("omneval-sdk")
        func_name = fn.__name__

        # Check if there's an active parent span from a caller.
        parent_span = _current_span.get()

        # Use the parent span's context if available, otherwise root context.
        if parent_span is not None:
            ctx = otel_trace.set_span_in_context(parent_span)
        else:
            ctx = otel_context.get_current()

        with tracer.start_as_current_span(func_name, context=ctx) as span:
            # Store this span in the context variable for nested calls.
            token = _current_span.set(span)

            # Auto-apply active conversation ID if one is set.
            active_cid = _active_conversation_id.get()
            if active_cid is not None:
                set_conversation_id(span, active_cid)

            try:
                result = fn(*args, **kwargs)
                return result
            except Exception as exc:
                span.set_status(Status(StatusCode.ERROR))
                span.record_exception(exc)
                raise
            finally:
                _current_span.reset(token)

    return wrapper  # type: ignore[return-value]


def set_input(span: Span | None, input_value: str) -> None:
    """Attach input data to the span as the 'omneval.input' attribute.

    Silently returns if span is None (e.g. SDK not configured).
    """
    if span is None:
        return
    span.set_attribute("omneval.input", input_value)


def set_output(span: Span | None, output_value: str) -> None:
    """Attach output data to the span as the 'omneval.output' attribute.

    Silently returns if span is None (e.g. SDK not configured).
    """
    if span is None:
        return
    span.set_attribute("omneval.output", output_value)


def set_model(span: Span | None, model: str) -> None:
    """Attach model name to the span as the 'gen_ai.request.model' attribute.

    Silently returns if span is None (e.g. SDK not configured).
    """
    if span is None:
        return
    span.set_attribute("gen_ai.request.model", model)


def set_tokens(span: Span | None, input_tokens: int, output_tokens: int) -> None:
    """Attach token counts to the span as 'gen_ai.usage.input_tokens' and 'gen_ai.usage.output_tokens'.

    Silently returns if span is None (e.g. SDK not configured).
    """
    if span is None:
        return
    span.set_attribute("gen_ai.usage.input_tokens", input_tokens)
    span.set_attribute("gen_ai.usage.output_tokens", output_tokens)


def get_active_span() -> Optional[Span]:
    """Return the currently active span from the contextvar."""
    return _current_span.get()


def generate_span_id() -> str:
    """Generate a 16-character hex span ID (8 bytes)."""
    return uuid.uuid4().hex[:16]


def generate_trace_id() -> str:
    """Generate a 32-character hex trace ID (16 bytes)."""
    return uuid.uuid4().hex[:32]


def generate_conversation_id() -> str:
    """Generate a 32-character hex conversation ID (16 bytes).

    Per OTel GenAI semantic conventions (gen_ai.conversation.id).
    """
    return uuid.uuid4().hex[:32]


# Context variable for carrying the active conversation ID across nested spans.
_active_conversation_id: contextvars.ContextVar[Optional[str]] = (
    contextvars.ContextVar("_active_conversation_id", default=None)
)


def set_conversation_id(span: Span | None, conversation_id: str) -> None:
    """Attach a conversation ID to the span as 'gen_ai.conversation.id'.

    Silently returns if span is None (e.g. SDK not configured).
    """
    if span is None:
        return
    span.set_attribute("gen_ai.conversation.id", conversation_id)


def set_active_conversation_id(conversation_id: str) -> None:
    """Set the active conversation ID on the current span and store it in
    the context variable so nested spans automatically inherit it.

    If no span is active, the ID is stored in the contextvar for the next
    span that starts.
    """
    _active_conversation_id.set(conversation_id)
    span = get_active_span()
    if span is not None:
        set_conversation_id(span, conversation_id)


def get_active_conversation_id() -> Optional[str]:
    """Return the currently active conversation ID from the contextvar."""
    return _active_conversation_id.get()
