import functools
from typing import Callable, TypeVar

F = TypeVar("F", bound=Callable)


def trace(fn: F) -> F:
    """Decorator that wraps a function in an OTel span sent to Lantern."""
    @functools.wraps(fn)
    def wrapper(*args, **kwargs):
        raise NotImplementedError
    return wrapper  # type: ignore
