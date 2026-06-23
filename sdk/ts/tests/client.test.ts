import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { OmnevalClient } from "../src/client";
import { mockFetch, createResponse } from "./utils";

describe("OmnevalClient", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  describe("getPrompt", () => {
    it("fetches a prompt by label", async () => {
      const fetchSpy = mockFetch(async (url) => {
        expect(url).toContain("/api/v1/prompts/greeting?label=production");
        return createResponse(200, {
          name: "greeting",
          version: 1,
          template: "Hello, {{.Name}}!",
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const template = await client.getPrompt("greeting", "production");

      expect(template).toBe("Hello, {{.Name}}!");
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("defaults label to production", async () => {
      const fetchSpy = mockFetch(async (url) => {
        expect(url).toContain("?label=production");
        return createResponse(200, {
          name: "test",
          version: 1,
          template: "test",
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.getPrompt("test");

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("caches prompt results for 30 seconds", async () => {
      const fetchSpy = mockFetch(async () => {
        return createResponse(200, {
          name: "cached",
          version: 1,
          template: "cached content",
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.getPrompt("cached", "production");
      await client.getPrompt("cached", "production");
      await client.getPrompt("cached", "production");

      // Only one fetch call due to caching
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("throws on 404", async () => {
      mockFetch(async () => {
        return createResponse(404);
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(
        client.getPrompt("nonexistent", "production")
      ).rejects.toThrow("prompt not found");
    });

    it("throws on server error", async () => {
      mockFetch(async () => {
        return createResponse(500, { error: "internal error" });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(
        client.getPrompt("test", "production")
      ).rejects.toThrow("get prompt: 500:");
    });

    it("fetches by version", async () => {
      const fetchSpy = mockFetch(async (url) => {
        expect(url).toContain("?version=2");
        return createResponse(200, {
          name: "v2",
          version: 2,
          template: "version 2 content",
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const template = await client.getPrompt("v2", { version: 2 });

      expect(template).toBe("version 2 content");
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("caches version results with no TTL", async () => {
      const fetchSpy = mockFetch(async () => {
        return createResponse(200, {
          name: "immutable",
          version: 1,
          template: "immutable content",
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.getPrompt("immutable", { version: 1 });
      await client.getPrompt("immutable", { version: 1 });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });
  });

  describe("writeScore", () => {
    it("writes a score", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.span_id).toBe("span-abc");
        expect(body.eval_name).toBe("helpfulness");
        expect(body.value).toBe(0.8);
        expect(body.reasoning).toBe("Great answer");
        return createResponse(201, { score_id: "score-123" });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.writeScore("span-abc", {
        name: "helpfulness",
        value: 0.8,
        reasoning: "Great answer",
      });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("throws on empty span ID", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(
        client.writeScore("", { name: "eval", value: 1.0 })
      ).rejects.toThrow("span_id is required");
    });

    it("throws on server error", async () => {
      mockFetch(async () => {
        return createResponse(500, { error: "internal error" });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(
        client.writeScore("span-1", { name: "eval", value: 1.0 })
      ).rejects.toThrow("write score: 500:");
    });

    it("includes API key header when configured", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        return createResponse(201);
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_proj_test",
      });
      await client.writeScore("span-1", { name: "eval", value: 1.0 });

      const call = fetchSpy.mock.calls[0];
      const headers = (call[1] as RequestInit)?.headers as Record<string, string>;
      expect(headers["X-API-Key"]).toBe("oev_proj_test");
    });

    it("includes project_id extracted from oev_proj_ api key in score payload", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.project_id).toBe("myprojectsuffix");
        return createResponse(201);
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_proj_myprojectsuffix",
      });
      await client.writeScore("span-1", { name: "eval", value: 1.0 });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("includes project_id extracted from oev_svc_ api key in score payload", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.project_id).toBe("myservicesuffix");
        return createResponse(201);
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_svc_myservicesuffix",
      });
      await client.writeScore("span-1", { name: "eval", value: 1.0 });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("uses full api key as project_id when key has no recognized prefix", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.project_id).toBe("raw-key");
        return createResponse(201);
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "raw-key",
      });
      await client.writeScore("span-1", { name: "eval", value: 1.0 });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("uses empty string as project_id when no api key configured", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.project_id).toBe("");
        return createResponse(201);
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.writeScore("span-1", { name: "eval", value: 1.0 });

      expect(fetchSpy).toHaveBeenCalledOnce();
    });
  });

  describe("createPrompt", () => {
    it("posts to /api/v1/prompts and returns prompt data", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.name).toBe("greeting");
        expect(body.template).toBe("Hello, {{.Name}}!");
        return createResponse(201, {
          name: "greeting",
          version: 1,
          template: "Hello, {{.Name}}!",
          model: "gpt-4",
          temperature: 0.7,
          max_tokens: 100,
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.createPrompt("greeting", "Hello, {{.Name}}!", {
        model_config: { model: "gpt-4", temperature: 0.7, max_tokens: 100 },
      });

      expect(result.name).toBe("greeting");
      expect(result.version).toBe(1);
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("sends X-API-Key header when configured", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const headers = (init?.headers as Record<string, string>) ?? {};
        expect(headers["X-API-Key"]).toBe("oev_proj_test");
        return createResponse(201, { name: "test", version: 1, template: "t" });
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_proj_test",
      });
      await client.createPrompt("test", "template");

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("throws on empty name", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.createPrompt("", "template")).rejects.toThrow("name is required");
    });

    it("throws on empty template", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.createPrompt("my-prompt", "")).rejects.toThrow("template is required");
    });

    it("throws on server error", async () => {
      mockFetch(async () => createResponse(409, { error: "version already exists" }));

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.createPrompt("greeting", "Hello")).rejects.toThrow("create prompt: 409:");
    });
  });

  describe("listPrompts", () => {
    it("returns list of prompt summaries", async () => {
      const fetchSpy = mockFetch(async (url) => {
        expect(url).toContain("/api/v1/prompts");
        return createResponse(200, [
          { name: "greeting", latest_version: 2, labels: { production: 2, staging: 1 } },
          { name: "eval", latest_version: 1, labels: {} },
        ]);
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.listPrompts();

      expect(result).toHaveLength(2);
      expect(result[0].name).toBe("greeting");
      expect(result[0].latest_version).toBe(2);
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("sends X-API-Key header when configured", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const headers = (init?.headers as Record<string, string>) ?? {};
        expect(headers["X-API-Key"]).toBe("oev_proj_test");
        return createResponse(200, []);
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_proj_test",
      });
      await client.listPrompts();

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("returns empty array when no prompts", async () => {
      mockFetch(async () => createResponse(200, []));

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.listPrompts();
      expect(result).toEqual([]);
    });
  });

  describe("setPromptLabel", () => {
    it("sends PUT to /api/v1/prompts/:name/labels/:label", async () => {
      const fetchSpy = mockFetch(async (url, init) => {
        expect(url).toContain("/api/v1/prompts/greeting/labels/production");
        expect(init?.method).toBe("PUT");
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.version).toBe(2);
        return createResponse(200, { name: "greeting", label: "production", version: 2 });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await client.setPromptLabel("greeting", "production", 2);

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("sends X-API-Key header when configured", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const headers = (init?.headers as Record<string, string>) ?? {};
        expect(headers["X-API-Key"]).toBe("oev_proj_test");
        return createResponse(200, { name: "test", label: "production", version: 1 });
      });

      const client = new OmnevalClient({
        baseUrl: "http://localhost:3000",
        apiKey: "oev_proj_test",
      });
      await client.setPromptLabel("test", "production", 1);

      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("throws on empty name", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.setPromptLabel("", "production", 1)).rejects.toThrow("name is required");
    });

    it("throws on server error", async () => {
      mockFetch(async () => createResponse(404, { error: "not found" }));

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.setPromptLabel("nonexistent", "production", 99)).rejects.toThrow(
        "set prompt label: 404:"
      );
    });
  });

  describe("listEvalRules", () => {
    it("returns list of eval rules", async () => {
      const fetchSpy = mockFetch(async (url) => {
        expect(url).toContain("/api/v1/eval-rules");
        return createResponse(200, {
          rules: [
            {
              rule_id: "rule-1",
              name: "helpfulness",
              judge_model: "gpt-4o-mini",
              prompt_name: "eval-prompt",
              prompt_version: 1,
              filter: {},
              sample_rate: 1.0,
              enabled: true,
            },
          ],
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.listEvalRules();

      expect(result).toHaveLength(1);
      expect(result[0].name).toBe("helpfulness");
      expect(result[0].rule_id).toBe("rule-1");
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("returns empty array when no rules", async () => {
      mockFetch(async () => createResponse(200, { rules: [] }));

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.listEvalRules();
      expect(result).toEqual([]);
    });
  });

  describe("createEvalRule", () => {
    it("posts to /api/v1/eval-rules and returns rule data", async () => {
      const fetchSpy = mockFetch(async (_url, init) => {
        const body = JSON.parse((init?.body as string) ?? "{}");
        expect(body.name).toBe("helpfulness");
        expect(body.prompt_name).toBe("eval-prompt");
        return createResponse(201, {
          rule_id: "rule-123",
          name: "helpfulness",
          judge_model: "gpt-4o-mini",
          prompt_name: "eval-prompt",
          prompt_version: 1,
          filter: {},
          sample_rate: 1.0,
          enabled: true,
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.createEvalRule("helpfulness", "eval-prompt");

      expect(result.rule_id).toBe("rule-123");
      expect(result.name).toBe("helpfulness");
      expect(fetchSpy).toHaveBeenCalledOnce();
    });

    it("throws on empty name", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.createEvalRule("", "eval-prompt")).rejects.toThrow("name is required");
    });

    it("throws on empty prompt_name", async () => {
      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      await expect(client.createEvalRule("helpfulness", "")).rejects.toThrow(
        "prompt_name is required"
      );
    });

    it("includes prompt_label in payload when provided", async () => {
      let capturedBody: Record<string, unknown> = {};
      const fetchSpy = mockFetch(async (_url, init) => {
        capturedBody = JSON.parse((init?.body as string) ?? "{}");
        return createResponse(201, {
          rule_id: "rule-456",
          name: "helpfulness",
          judge_model: "gpt-4o-mini",
          prompt_name: "eval-prompt",
          prompt_version: 3,
          prompt_label: "production",
          filter: {},
          sample_rate: 1.0,
          enabled: true,
        });
      });

      const client = new OmnevalClient({ baseUrl: "http://localhost:3000" });
      const result = await client.createEvalRule("helpfulness", "eval-prompt", {
        prompt_version: 3,
        prompt_label: "production",
      });

      expect(capturedBody.prompt_version).toBe(3);
      expect(capturedBody.prompt_label).toBe("production");
      expect(result.prompt_label).toBe("production");
      expect(fetchSpy).toHaveBeenCalledOnce();
    });
  });
});
