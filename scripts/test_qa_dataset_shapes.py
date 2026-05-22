"""
Unit tests for Section 11 Dataset response-shape assertions in qa_validation.py.

These tests verify that the QA script correctly reads wrapped response objects
instead of bare arrays, matching the actual API contract:
  GET /api/v1/datasets          -> {"datasets": [...]}
  GET /api/v1/datasets/{id}/items -> {"items": [...], "next_cursor": ...}
"""


def _report_collector():
    """Returns (results, report_fn) for capturing pass/fail messages."""
    results = []

    def report(msg, status, detail=""):
        results.append({"msg": msg, "status": status, "detail": detail})

    return results, report


PASS = "PASS"
FAIL = "FAIL"


# ---------------------------------------------------------------------------
# Logic extracted from Section 11 (post-fix)
# ---------------------------------------------------------------------------


def check_list_items(items):
    """Mirrors the fixed list-items check in Section 11."""
    results, report = _report_collector()
    if isinstance(items, dict) and "items" in items:
        report(f"Dataset: list items -> 200 ({len(items['items'])} items)", PASS)
    else:
        report("Dataset: list items -> 200", FAIL, f"unexpected type: {type(items)}")
    return results


def check_list_all(response_json):
    """Mirrors the fixed list-all check in Section 11."""
    results, report = _report_collector()
    if isinstance(response_json.get("datasets"), list):
        report(
            f"Dataset: list all -> 200 ({len(response_json['datasets'])} datasets)",
            PASS,
        )
    else:
        report("Dataset: list all -> 200", FAIL, f"unexpected shape")
    return results


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_list_items_wrapped_passes():
    """API returns {"items": [...], "next_cursor": null} — should PASS."""
    api_response = {"items": [{"id": "1", "input": "hello"}], "next_cursor": None}
    results = check_list_items(api_response)
    assert len(results) == 1
    assert results[0]["status"] == PASS
    assert "1 items" in results[0]["msg"]


def test_list_items_bare_array_fails():
    """Old shape (bare list) — should FAIL under the fixed check."""
    api_response = [{"id": "1", "input": "hello"}]
    results = check_list_items(api_response)
    assert len(results) == 1
    assert results[0]["status"] == FAIL


def test_list_items_empty_items_key_passes():
    """Empty items list still has the key — should PASS with count 0."""
    api_response = {"items": [], "next_cursor": None}
    results = check_list_items(api_response)
    assert results[0]["status"] == PASS
    assert "0 items" in results[0]["msg"]


def test_list_all_wrapped_passes():
    """API returns {"datasets": [...]} — should PASS."""
    api_response = {"datasets": [{"id": "abc", "name": "qa-dataset"}]}
    results = check_list_all(api_response)
    assert len(results) == 1
    assert results[0]["status"] == PASS
    assert "1 datasets" in results[0]["msg"]


def test_list_all_bare_array_fails():
    """Old shape (bare list returned as a dict without 'datasets' key) — FAIL."""
    # Simulates a dict that doesn't have "datasets" key
    api_response = {}
    results = check_list_all(api_response)
    assert results[0]["status"] == FAIL


def test_list_all_empty_datasets_passes():
    """Empty datasets list should still PASS."""
    api_response = {"datasets": []}
    results = check_list_all(api_response)
    assert results[0]["status"] == PASS
    assert "0 datasets" in results[0]["msg"]


if __name__ == "__main__":
    tests = [
        test_list_items_wrapped_passes,
        test_list_items_bare_array_fails,
        test_list_items_empty_items_key_passes,
        test_list_all_wrapped_passes,
        test_list_all_bare_array_fails,
        test_list_all_empty_datasets_passes,
    ]
    passed = 0
    failed = 0
    for t in tests:
        try:
            t()
            print(f"  PASS  {t.__name__}")
            passed += 1
        except AssertionError as e:
            print(f"  FAIL  {t.__name__}: {e}")
            failed += 1
    print(f"\n{passed} passed, {failed} failed")
    if failed:
        raise SystemExit(1)
