class LanternClient:
    """HTTP client for prompt fetch and manual score writes.
    Prompt responses are cached client-side with a short TTL.
    """

    def __init__(self, base_url: str, api_key: str) -> None:
        raise NotImplementedError

    def get_prompt(self, name: str, label: str = "production") -> dict:
        raise NotImplementedError

    def write_score(self, span_id: str, eval_name: str, value: float, reasoning: str = "") -> None:
        raise NotImplementedError
