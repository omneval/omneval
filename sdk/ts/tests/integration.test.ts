import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createOmneval } from "../src/omneval";
import { mockFetch, createResponse } from "./utils";

describe("Omneval SDK Integration", () => {
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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000", apiKey: "oev_proj_test" });

    const spanId = omneval.startSpan("llm.call", { kind: "llm" });
    omneval.setModel(spanId, "gpt-4");
    omneval.setInput(spanId, "Hello!");
    omneval.setTokens(spanId, 10, 5);

    await omneval.endSpan(spanId, "Hi there!");

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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const spanId = omneval.startSpan("test.span");
    await omneval.endSpan(spanId, { output: "response", attributes: { custom: "attr" } });

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: multiple spans in sequence", async () => {
    const fetchCalls: any[] = [];
    mockFetch(async (url, init) => {
      fetchCalls.push(JSON.parse((init?.body as string) ?? "{}"));
      return createResponse(202);
    });

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const id1 = omneval.startSpan("span-1");
    omneval.setModel(id1, "gpt-4");
    await omneval.endSpan(id1, "output-1");

    const id2 = omneval.startSpan("span-2");
    await omneval.endSpan(id2, "output-2");

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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const t1 = await omneval.getPrompt("greeting", "production");
    const t2 = await omneval.getPrompt("greeting", "production");
    const t3 = await omneval.getPrompt("greeting", "staging");

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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const t1 = await omneval.getPromptByVersion("test", 1);
    const t2 = await omneval.getPromptByVersion("test", 1);

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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    await omneval.writeScore("span-abc", {
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

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    await omneval.writeScore("span-1", "helpfulness", 0.8);

    expect(fetchSpy).toHaveBeenCalledOnce();
  });

  it("end-to-end: flush sends only ended pending spans", async () => {
    const fetchCalls: any[] = [];
    mockFetch(async (url, init) => {
      const body = JSON.parse((init?.body as string) ?? "{}");
      fetchCalls.push(body);
      return createResponse(202);
    });

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const id1 = omneval.startSpan("span-1");
    omneval.setModel(id1, "gpt-4");
    const id2 = omneval.startSpan("span-2");

    // End both spans, then flush
    await omneval.endSpan(id1, "output-1");
    await omneval.endSpan(id2, "output-2");

    expect(fetchCalls).toHaveLength(2);

    // Starting another span and flushing should send it
    const id3 = omneval.startSpan("span-3");
    await omneval.endSpan(id3, "output-3");
    expect(fetchCalls).toHaveLength(3);
  });

  it("end-to-end: setPrompt with just name", async () => {
    const fetchSpy = mockFetch(async (url, init) => {
      const body = JSON.parse((init?.body as string) ?? "{}");
      expect(body.spans[0].prompt_name).toBe("greeting");
      expect(body.spans[0].prompt_version).toBeUndefined();
      return createResponse(202);
    });

    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });

    const spanId = omneval.startSpan("test.span");
    omneval.setPrompt(spanId, "greeting");
    await omneval.endSpan(spanId);

    expect(fetchSpy).toHaveBeenCalledOnce();
  });
});
