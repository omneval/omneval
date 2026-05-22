"""Tests for OmnevalClient."""
import json
import time

import requests
import responses

from omneval_sdk.client import OmnevalClient


class TestGetPrompt:
    """Tests for OmnevalClient.get_prompt."""

    @responses.activate
    def test_get_prompt_returns_cached_value(self):
        """get_prompt returns the cached value within TTL without an HTTP call."""
        prompt_response = {
            "name": "greeting",
            "version": 1,
            "template": "Hello, {{.Name}}!",
            "model_config": {"model": "gpt-4", "temperature": 0.7, "max_tokens": 100},
        }

        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/greeting?label=production",
            json=prompt_response,
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")

        # First call — should hit the server.
        result1 = client.get_prompt("greeting", "production")
        assert result1["template"] == "Hello, {{.Name}}!"

        # Second call — should use cache (no additional HTTP request).
        result2 = client.get_prompt("greeting", "production")
        assert result2["template"] == "Hello, {{.Name}}!"

        # Only 1 HTTP request should have been made.
        assert len(responses.calls) == 1

    @responses.activate
    def test_get_prompt_with_label(self):
        """get_prompt fetches prompt by name and label."""
        prompt_response = {
            "name": "eval",
            "version": 2,
            "template": "Evaluate: {{.Input}}",
            "model_config": {"model": "gpt-4"},
        }

        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/eval?label=staging",
            json=prompt_response,
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.get_prompt("eval", "staging")
        assert result["template"] == "Evaluate: {{.Input}}"

    @responses.activate
    def test_get_prompt_default_label(self):
        """get_prompt defaults label to 'production'."""
        captured_label = None

        def capture_request(request):
            nonlocal captured_label
            captured_label = request.url.split("label=")[-1]
            return (
                200,
                {},
                json.dumps({"name": "test", "version": 1, "template": "test", "model_config": {}}),
            )

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/test",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.get_prompt("test", "")  # empty label should default to production

        assert captured_label == "production"


class TestWriteScore:
    """Tests for OmnevalClient.write_score."""

    @responses.activate
    def test_write_score_posts_to_endpoint(self):
        """write_score posts to POST /api/v1/scores."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"score_id": "score-123"},
            status=201,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.write_score("span-abc", "helpfulness", 0.8, "Great answer")

        assert len(responses.calls) == 1
        req = responses.calls[0].request
        assert req.method == "POST"
        assert "/api/v1/scores" in req.url

        body = json.loads(req.body)
        assert body["span_id"] == "span-abc"
        assert body["eval_name"] == "helpfulness"
        assert body["value"] == 0.8
        assert body["reasoning"] == "Great answer"
        assert "trace_id" in body  # SDK generates trace_id

    @responses.activate
    def test_write_score_with_empty_reasoning(self):
        """write_score works with empty reasoning string."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"score_id": "score-123"},
            status=201,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.write_score("span-xyz", "accuracy", 1.0)

        body = json.loads(responses.calls[0].request.body)
        assert body["reasoning"] == ""

    def test_write_score_requires_span_id(self):
        """write_score raises ValueError for empty span_id."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.write_score("", "eval", 1.0)
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "span_id is required" in str(e)

    @responses.activate
    def test_write_score_includes_project_id(self):
        """write_score automatically includes project_id in the request payload."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"score_id": "score-456"},
            status=201,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.write_score("span-abc", "helpfulness", 0.8, "Great answer")

        body = json.loads(responses.calls[0].request.body)
        assert "project_id" in body, "project_id must be in the request payload"
        assert body["project_id"] == "test"

    @responses.activate
    def test_write_score_400_without_project_id(self):
        """write_score raises an HTTPError when the server returns 400 (missing project_id)."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"error": "span_id, trace_id, and project_id are required"},
            status=400,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.write_score("span-abc", "helpfulness", 0.8)
            assert False, "Expected requests.HTTPError"
        except requests.HTTPError as e:
            assert e.response.status_code == 400

    @responses.activate
    def test_write_score_500_error(self):
        """write_score raises an HTTPError when the server returns 500."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"error": "internal error"},
            status=500,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.write_score("span-abc", "helpfulness", 0.8)
            assert False, "Expected requests.HTTPError"
        except requests.HTTPError as e:
            assert e.response.status_code == 500

    @responses.activate
    def test_write_score_with_explicit_project_id(self):
        """write_score uses the explicit project_id when provided."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            json={"score_id": "score-789"},
            status=201,
        )

        client = OmnevalClient(
            "http://localhost:8080",
            "oev_proj_key123",
            project_id="explicit-project",  # override
        )
        client.write_score("span-abc", "helpfulness", 0.8)

        body = json.loads(responses.calls[0].request.body)
        assert body["project_id"] == "explicit-project"

    def test_project_id_extracted_from_project_key(self):
        """project_id is extracted from oev_proj_ prefix keys."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_my-project-123")
        assert client._project_id == "my-project-123"

    def test_project_id_extracted_from_service_key(self):
        """project_id is extracted from oev_svc_ prefix keys."""
        client = OmnevalClient("http://localhost:8080", "oev_svc_my-service")
        assert client._project_id == "my-service"

    def test_project_id_override(self):
        """Explicit project_id overrides the value extracted from api_key."""
        client = OmnevalClient(
            "http://localhost:8080",
            "oev_proj_key123",
            project_id="override-id",
        )
        assert client._project_id == "override-id"


class TestAPIKeyHeaders:
    """Tests for X-API-Key header on API requests (issue #5)."""

    @responses.activate
    def test_write_score_includes_api_key_header(self):
        """write_score sends X-API-Key header when api_key is configured."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (201, {}, json.dumps({"score_id": "score-123"}))

        responses.add_callback(
            responses.POST,
            "http://localhost:8080/api/v1/scores",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.write_score("span-abc", "helpfulness", 0.8, "Great answer")

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    @responses.activate
    def test_get_prompt_includes_api_key_header(self):
        """get_prompt sends X-API-Key header when api_key is configured."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (
                200,
                {},
                json.dumps({"name": "test", "version": 1, "template": "test", "model_config": {}}),
            )

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/test",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.get_prompt("test", "production")

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    @responses.activate
    def test_get_prompt_version_includes_api_key_header(self):
        """get_prompt_version sends X-API-Key header when api_key is configured."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (
                200,
                {},
                json.dumps({"name": "test", "version": 2, "template": "v2", "model_config": {}}),
            )

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/test",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.get_prompt_version("test", 2)

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    @responses.activate
    def test_no_api_key_omits_header(self):
        """No X-API-Key header is sent when api_key is empty."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (
                200,
                {},
                json.dumps({"name": "test", "version": 1, "template": "test", "model_config": {}}),
            )

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/test",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080")  # no api_key
        client.get_prompt("test", "production")

        assert captured_headers is not None
        assert "X-API-Key" not in captured_headers


class TestCreatePrompt:
    """Tests for OmnevalClient.create_prompt."""

    @responses.activate
    def test_create_prompt_posts_to_endpoint(self):
        """create_prompt posts to POST /api/v1/prompts."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/prompts",
            json={
                "name": "greeting",
                "version": 1,
                "template": "Hello, {{.Name}}!",
                "model": "gpt-4",
                "temperature": 0.7,
                "max_tokens": 100,
            },
            status=201,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.create_prompt("greeting", "Hello, {{.Name}}!", model_config={"model": "gpt-4"})

        assert len(responses.calls) == 1
        req = responses.calls[0].request
        assert req.method == "POST"
        assert "/api/v1/prompts" in req.url
        body = json.loads(req.body)
        assert body["name"] == "greeting"
        assert body["template"] == "Hello, {{.Name}}!"
        assert result["name"] == "greeting"
        assert result["version"] == 1

    @responses.activate
    def test_create_prompt_sends_api_key_header(self):
        """create_prompt sends X-API-Key header."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (201, {}, json.dumps({"name": "test", "version": 1, "template": "t", "model": "", "temperature": 0, "max_tokens": 0}))

        responses.add_callback(
            responses.POST,
            "http://localhost:8080/api/v1/prompts",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.create_prompt("test", "template")

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    def test_create_prompt_requires_name(self):
        """create_prompt raises ValueError for empty name."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.create_prompt("", "template")
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "name is required" in str(e)

    def test_create_prompt_requires_template(self):
        """create_prompt raises ValueError for empty template."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.create_prompt("my-prompt", "")
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "template is required" in str(e)


class TestListPrompts:
    """Tests for OmnevalClient.list_prompts."""

    @responses.activate
    def test_list_prompts_returns_list(self):
        """list_prompts returns a list of prompt summaries."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts",
            json=[
                {"name": "greeting", "latest_version": 2, "labels": {"production": 2, "staging": 1}},
                {"name": "eval", "latest_version": 1, "labels": {}},
            ],
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.list_prompts()

        assert len(result) == 2
        assert result[0]["name"] == "greeting"
        assert result[0]["latest_version"] == 2
        assert result[1]["name"] == "eval"

    @responses.activate
    def test_list_prompts_sends_api_key_header(self):
        """list_prompts sends X-API-Key header."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (200, {}, json.dumps([]))

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/prompts",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.list_prompts()

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    @responses.activate
    def test_list_prompts_returns_empty_list(self):
        """list_prompts returns an empty list when no prompts exist."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts",
            json=[],
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.list_prompts()
        assert result == []


class TestSetPromptLabel:
    """Tests for OmnevalClient.set_prompt_label."""

    @responses.activate
    def test_set_prompt_label_puts_to_endpoint(self):
        """set_prompt_label sends PUT to /api/v1/prompts/:name/labels/:label."""
        responses.add(
            responses.PUT,
            "http://localhost:8080/api/v1/prompts/greeting/labels/production",
            json={"name": "greeting", "label": "production", "version": 2},
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.set_prompt_label("greeting", "production", 2)

        assert len(responses.calls) == 1
        req = responses.calls[0].request
        assert req.method == "PUT"
        assert "/api/v1/prompts/greeting/labels/production" in req.url
        body = json.loads(req.body)
        assert body["version"] == 2

    @responses.activate
    def test_set_prompt_label_sends_api_key_header(self):
        """set_prompt_label sends X-API-Key header."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (200, {}, json.dumps({"name": "test", "label": "production", "version": 1}))

        responses.add_callback(
            responses.PUT,
            "http://localhost:8080/api/v1/prompts/test/labels/production",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.set_prompt_label("test", "production", 1)

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    def test_set_prompt_label_requires_name(self):
        """set_prompt_label raises ValueError for empty name."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.set_prompt_label("", "production", 1)
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "name is required" in str(e)


class TestListEvalRules:
    """Tests for OmnevalClient.list_eval_rules."""

    @responses.activate
    def test_list_eval_rules_returns_list(self):
        """list_eval_rules returns a list of eval rules."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/eval-rules",
            json={
                "rules": [
                    {
                        "rule_id": "rule-1",
                        "name": "helpfulness",
                        "judge_model": "gpt-4o-mini",
                        "prompt_name": "eval-prompt",
                        "prompt_version": 1,
                        "filter": {},
                        "sample_rate": 1.0,
                        "enabled": True,
                    }
                ]
            },
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.list_eval_rules()

        assert len(result) == 1
        assert result[0]["name"] == "helpfulness"
        assert result[0]["rule_id"] == "rule-1"

    @responses.activate
    def test_list_eval_rules_sends_api_key_header(self):
        """list_eval_rules sends X-API-Key header."""
        captured_headers = None

        def capture_request(request):
            nonlocal captured_headers
            captured_headers = dict(request.headers)
            return (200, {}, json.dumps({"rules": []}))

        responses.add_callback(
            responses.GET,
            "http://localhost:8080/api/v1/eval-rules",
            callback=capture_request,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        client.list_eval_rules()

        assert captured_headers is not None
        assert captured_headers.get("X-API-Key") == "oev_proj_test"

    @responses.activate
    def test_list_eval_rules_returns_empty_list(self):
        """list_eval_rules returns empty list when no rules exist."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/eval-rules",
            json={"rules": []},
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.list_eval_rules()
        assert result == []


class TestCreateEvalRule:
    """Tests for OmnevalClient.create_eval_rule."""

    @responses.activate
    def test_create_eval_rule_posts_to_endpoint(self):
        """create_eval_rule posts to POST /api/v1/eval-rules."""
        responses.add(
            responses.POST,
            "http://localhost:8080/api/v1/eval-rules",
            json={
                "rule_id": "rule-123",
                "name": "helpfulness",
                "judge_model": "gpt-4o-mini",
                "prompt_name": "eval-prompt",
                "prompt_version": 1,
                "filter": {},
                "sample_rate": 1.0,
                "enabled": True,
            },
            status=201,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.create_eval_rule("helpfulness", "eval-prompt", filter={"model": "gpt-4"})

        assert len(responses.calls) == 1
        req = responses.calls[0].request
        assert req.method == "POST"
        assert "/api/v1/eval-rules" in req.url
        body = json.loads(req.body)
        assert body["name"] == "helpfulness"
        assert body["prompt_name"] == "eval-prompt"
        assert result["rule_id"] == "rule-123"

    def test_create_eval_rule_requires_name(self):
        """create_eval_rule raises ValueError for empty name."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.create_eval_rule("", "eval-prompt")
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "name is required" in str(e)

    def test_create_eval_rule_requires_prompt_name(self):
        """create_eval_rule raises ValueError for empty prompt_name."""
        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        try:
            client.create_eval_rule("helpfulness", "")
            assert False, "Expected ValueError"
        except ValueError as e:
            assert "prompt_name is required" in str(e)


class TestGetPromptVersion:
    """Tests for OmnevalClient.get_prompt_version."""

    @responses.activate
    def test_get_prompt_version_fetches_by_version(self):
        """get_prompt_version fetches a prompt by explicit version number."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/greeting?version=2",
            json={"name": "greeting", "version": 2, "template": "Welcome!", "model_config": {}},
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")
        result = client.get_prompt_version("greeting", 2)
        assert result["template"] == "Welcome!"
        assert result["version"] == 2

    @responses.activate
    def test_get_prompt_version_cached(self):
        """get_prompt_version caches indefinitely (no TTL)."""
        responses.add(
            responses.GET,
            "http://localhost:8080/api/v1/prompts/greeting?version=1",
            json={"name": "greeting", "version": 1, "template": "Hello!", "model_config": {}},
            status=200,
        )

        client = OmnevalClient("http://localhost:8080", "oev_proj_test")

        # First call — hits the server.
        client.get_prompt_version("greeting", 1)
        # Second call — uses cache.
        client.get_prompt_version("greeting", 1)

        assert len(responses.calls) == 1
