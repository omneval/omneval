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
  });
});
