import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createLantern } from "../src/lantern";
import { mockFetch, createResponse } from "./utils";

describe("Lantern SDK Integration", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it("end-to-end: start span, set attributes, end span, export", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      expect(url).toBe("http://localhost:3000/api/v1/spans");
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.spans).toHaveLength(1);
      expect(body.spans[0].name).toBe("llm.call");
      expect(body.spans[0].model).toBe("gpt-4");
      expect(body.spans[0].input).toBe("Hello!");
      expect(body.spans[0].output).toBe("Hi there!");
      expect(body.spans[0].input_tokens).toBe(10);
      expect(body.spans[0].output_tokens).toBe(5);
      return createResponse(202);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000", apiKey: "ltn_proj_test" });

    const spanId = lantern.startSpan("llm.call", { kind: "llm" });
    lantern.setModel(spanId, "gpt-4");
    lantern.setInput(spanId, "Hello!");
    lantern.setTokens(spanId, 10, 5);

    await lantern.endSpan(spanId, "Hi there!");

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: start span with output object", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      expect(url).toBe("http://localhost:3000/api/v1/spans");
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.spans[0].output).toBe("response");
      expect(body.spans[0].attributes).toEqual({ custom: "attr" });
      return createResponse(202);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const spanId = lantern.startSpan("test.span");
    await lantern.endSpan(spanId, { output: "response", attributes: { custom: "attr" } });

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: multiple spans in sequence", async () => {
    const fetchCalls: any[] = [];
    mockFetch(async (url, init) => {
      fetchCalls.push(JSON.parse((init?.body as string) ?? "{}"));
      return createResponse(202);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const id1 = lantern.startSpan("span-1");
    lantern.setModel(id1, "gpt-4");
    await lantern.endSpan(id1, "output-1");

    const id2 = lantern.startSpan("span-2");
    await lantern.endSpan(id2, "output-2");

    expect(fetchCalls).toHaveLength(2);
  });

  it("end-to-end: getPrompt caches and returns template", async () => {
    let callCount = 0;
    mockFetch(async (url) => {
      callCount++;
      return createResponse(200, {
        name: "greeting",
        version: 1,
        template: "Hello, {{.Name}}!",
      });
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const t1 = await lantern.getPrompt("greeting", "production");
    const t2 = await lantern.getPrompt("greeting", "production");
    const t3 = await lantern.getPrompt("greeting", "staging");

    expect(t1).toBe("Hello, {{.Name}}!");
    expect(t2).toBe("Hello, {{.Name}}!");
    // Different label — not cached
    expect(t3).toBe("Hello, {{.Name}}!");
    expect(callCount).toBe(2); // production + staging
  });

  it("end-to-end: getPromptByVersion caches with no TTL", async () => {
    let callCount = 0;
    mockFetch(async () => {
      callCount++;
      return createResponse(200, {
        name: "test",
        version: 1,
        template: "v1",
      });
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const t1 = await lantern.getPromptByVersion("test", 1);
    const t2 = await lantern.getPromptByVersion("test", 1);

    expect(t1).toBe("v1");
    expect(t2).toBe("v1");
    expect(callCount).toBe(1);
  });

  it("end-to-end: writeScore sends score and generates trace_id", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      expect(url).toBe("http://localhost:3000/api/v1/scores");
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.span_id).toBe("span-abc");
      expect(body.eval_name).toBe("accuracy");
      expect(body.value).toBe(0.95);
      expect(body.reasoning).toBe("Perfect answer");
      expect(body.trace_id).toMatch(/^[0-9a-f]{32}$/);
      return createResponse(201);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    await lantern.writeScore("span-abc", {
      name: "accuracy",
      value: 0.95,
      reasoning: "Perfect answer",
    });

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: writeScore shorthand syntax", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.eval_name).toBe("helpfulness");
      expect(body.value).toBe(0.8);
      return createResponse(201);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    await lantern.writeScore("span-1", "helpfulness", 0.8);

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: flush sends pending spans", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      const body = JSON.parse((init?.body as string) ?? "{}");
      return createResponse(202);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const id1 = lantern.startSpan("span-1");
    lantern.setModel(id1, "gpt-4");
    const id2 = lantern.startSpan("span-2");

    await lantern.flush();

    expect(fetchSpy).toHaveBeenCalledOnce();
    // After flush, pending spans should be cleared
    // Starting another span and flushing should only send the new one
    const id3 = lantern.startSpan("span-3");
    await lantern.flush();
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    expect(JSON.parse((fetchSpy.mock.calls[1][1]?.body as string))?.spans).toHaveLength(1);
  });

  it("end-to-end: setPrompt with just name", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.spans[0].prompt_name).toBe("greeting");
      expect(body.spans[0].prompt_version).toBeUndefined();
      return createResponse(202);
    });

    const lantern = createLantern();
    lantern.init({ baseUrl: "http://localhost:3000" });

    const spanId = lantern.startSpan("test.span");
    lantern.setPrompt(spanId, "greeting");
    await lantern.endSpan(spanId);

    expect(fetchSpy).toHaveBeenCalledOnce();
  });
});
