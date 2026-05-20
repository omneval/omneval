from omneval_sdk.trace import (
    trace,
    set_input,
    set_output,
    set_model,
    set_tokens,
    get_active_span,
)
from omneval_sdk.exporter import configure
from omneval_sdk.client import OmnevalClient

# Aliases for backwards compatibility and documentation alignment.
# The landing page quickstart shows `omneval.init(...)`, so `init` is
# exposed as an alias for `configure` to match the docs.
init = configure

__all__ = [
    "trace",
    "configure",
    "init",
    "OmnevalClient",
    "set_input",
    "set_output",
    "set_model",
    "set_tokens",
    "get_active_span",
]
