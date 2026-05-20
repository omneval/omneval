import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ManualTracer } from "../src/tracer";
import { createOmneval } from "../src/omneval";

describe("ManualTracer", () => {
  type ExportMock = {
    export: (spans: any[]) => Promise<boolean>;
  };

  function createMockExporter(spansCollector: (spans: any[]) => void): ExportMock {
    return {
      export: async (spans) => {
        spansCollector(spans);
        return true;
      },
    };
  }

  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it("startSpan returns a 16-char hex span ID", () => {
    const tracer = new ManualTracer({ export: async () => true });
    tracer.init();
    const spanId = tracer.startSpan("test.span");
    expect(spanId).toHaveLength(16);
    expect(spanId).toMatch(/^[0-9a-f]{16}$/);
  });

  it("endSpan sends spans to the exporter", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    await tracer.endSpan(spanId);

    expect(exportedSpans).toHaveLength(1);
    expect(exportedSpans[0].name).toBe("test.span");
    expect(exportedSpans[0].span_id).toBe(spanId);
  });

  it("setModel attaches model to span", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    tracer.setModel(spanId, "gpt-4");
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].model).toBe("gpt-4");
  });

  it("setInput attaches input to span", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    tracer.setInput(spanId, "hello world");
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].input).toBe("hello world");
  });

  it("setTokens attaches token counts to span", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    tracer.setTokens(spanId, 100, 50);
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].input_tokens).toBe(100);
    expect(exportedSpans[0].output_tokens).toBe(50);
  });

  it("setPrompt attaches prompt name/version to span", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    tracer.setPrompt(spanId, "greeting", 1);
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].prompt_name).toBe("greeting");
    expect(exportedSpans[0].prompt_version).toBe(1);
  });

  it("endSpan with output string", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span");
    await tracer.endSpan(spanId, { output: "response text" });

    expect(exportedSpans[0].output).toBe("response text");
  });

  it("endSpan ignores unknown span ID", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    await tracer.endSpan("unknown-span-id");
    expect(exportedSpans).toHaveLength(0);
  });

  it("endSpan with attributes merges with startSpan attributes", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("test.span", { attributes: { custom: "value" } });
    await tracer.endSpan(spanId, { attributes: { extra: "attr" } });

    expect(exportedSpans[0].attributes).toEqual({
      custom: "value",
      extra: "attr",
    });
  });

  it("flush sends only ended pending spans in one batch", async () => {
    const exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans.push(...s); }) as any);
    tracer.init();

    const id1 = tracer.startSpan("span-1");
    const id2 = tracer.startSpan("span-2");
    const id3 = tracer.startSpan("span-3");

    // End only span-1 and span-3, leave span-2 pending
    await tracer.endSpan(id1);
    await tracer.endSpan(id3);

    expect(exportedSpans).toHaveLength(2);
    expect(exportedSpans.map((s) => s.name)).toEqual(["span-1", "span-3"]);
  });

  it("span includes start_time and end_time", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("timed.span");
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].start_time).toBeDefined();
    expect(exportedSpans[0].end_time).toBeDefined();
    expect(exportedSpans[0].end_time).toBeGreaterThanOrEqual(exportedSpans[0].start_time);
  });

  it("span has trace_id matching the generated format", async () => {
    let exportedSpans: any[] = [];
    const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans = s; }) as any);
    tracer.init();

    const spanId = tracer.startSpan("trace.span");
    await tracer.endSpan(spanId);

    expect(exportedSpans[0].trace_id).toHaveLength(32);
    expect(exportedSpans[0].trace_id).toMatch(/^[0-9a-f]{32}$/);
  });

  describe("nested/parent-child spans", () => {
    it("ending a child span does not prematurely end the parent span", async () => {
      const exportedSpans: any[] = [];
      const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans.push(...s); }) as any);
      tracer.init();

      const parentId = tracer.startSpan("parent.span");
      const childId = tracer.startSpan("child.span", { parentSpanId: parentId });

      // End the child span first
      await tracer.endSpan(childId, { output: "child output" });

      // Parent should still be pending — not exported yet
      const parentExported = exportedSpans.find((s) => s.span_id === parentId);
      expect(parentExported).toBeUndefined();

      // Only the child span should have been exported
      const childExported = exportedSpans.find((s) => s.span_id === childId);
      expect(childExported).toBeDefined();
      expect(childExported!.output).toBe("child output");
      expect(childExported!.end_time).toBeDefined();
    });

    it("setting attributes on parent after child ended still works", async () => {
      const exportedSpans: any[] = [];
      const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans.push(...s); }) as any);
      tracer.init();

      const parentId = tracer.startSpan("parent.span");
      const childId = tracer.startSpan("child.span", { parentSpanId: parentId });

      // End the child span
      await tracer.endSpan(childId, { output: "child output" });

      // Now set attributes on the parent after child ended
      tracer.setModel(parentId, "gpt-4");
      tracer.setInput(parentId, "parent input");

      // End the parent
      await tracer.endSpan(parentId, { output: "parent output" });

      // Both spans should be exported
      expect(exportedSpans).toHaveLength(2);

      const parentExported = exportedSpans.find((s) => s.span_id === parentId);
      expect(parentExported).toBeDefined();
      expect(parentExported!.model).toBe("gpt-4");
      expect(parentExported!.input).toBe("parent input");
      expect(parentExported!.output).toBe("parent output");
      expect(parentExported!.end_time).toBeDefined();

      const childExported = exportedSpans.find((s) => s.span_id === childId);
      expect(childExported).toBeDefined();
      expect(childExported!.parent_id).toBe(parentId);
      expect(childExported!.end_time).toBeDefined();
      expect(childExported!.end_time).toBeLessThanOrEqual(parentExported!.end_time!);
    });

    it("flush only exports ended spans, not pending ones", async () => {
      const exportedSpans: any[] = [];
      const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans.push(...s); }) as any);
      tracer.init();

      const id1 = tracer.startSpan("span-1");
      const id2 = tracer.startSpan("span-2");
      const id3 = tracer.startSpan("span-3");

      // End only span-1 and span-3, leave span-2 pending
      await tracer.endSpan(id1);
      await tracer.endSpan(id3);

      // Only span-1 and span-3 should have been exported (each endSpan flushes)
      expect(exportedSpans).toHaveLength(2);
      expect(exportedSpans.map((s) => s.name)).toEqual(["span-1", "span-3"]);

      // span-2 should still be pending and exportable
      await tracer.endSpan(id2, { output: "late output" });

      // span-2 should now appear in exported spans
      const span2Exported = exportedSpans.find((s) => s.name === "span-2");
      expect(span2Exported).toBeDefined();
      expect(span2Exported!.output).toBe("late output");
    });

    it("nested spans maintain correct parent_id references", async () => {
      const exportedSpans: any[] = [];
      const tracer = new ManualTracer(createMockExporter((s) => { exportedSpans.push(...s); }) as any);
      tracer.init();

      const rootId = tracer.startSpan("root");
      const midId = tracer.startSpan("middle", { parentSpanId: rootId });
      const leafId = tracer.startSpan("leaf", { parentSpanId: midId });

      await tracer.endSpan(leafId, { output: "leaf" });
      await tracer.endSpan(midId, { output: "middle" });
      await tracer.endSpan(rootId, { output: "root" });

      const byId = (id: string) => exportedSpans.find((s) => s.span_id === id);

      expect(byId(rootId)!.parent_id).toBeUndefined();
      expect(byId(midId)!.parent_id).toBe(rootId);
      expect(byId(leafId)!.parent_id).toBe(midId);
      expect(byId(rootId)!.trace_id).toBe(byId(midId)!.trace_id);
      expect(byId(midId)!.trace_id).toBe(byId(leafId)!.trace_id);
    });
  });
});

describe("createOmneval", () => {
  it("creates a fresh OmnevalSDK instance", () => {
    const omneval = createOmneval();
    expect(omneval).toBeDefined();
    expect(omneval.config).toBeUndefined();
  });

  it("init() sets up the SDK", () => {
    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000", apiKey: "oev_proj_test" });
    expect(omneval.config).toBeDefined();
  });

  it("startSpan returns a span ID after init", () => {
    const omneval = createOmneval();
    omneval.init({ baseUrl: "http://localhost:3000" });
    const spanId = omneval.startSpan("test.span");
    expect(spanId).toHaveLength(16);
  });

  it("startSpan before init returns empty string with warning", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const omneval = createOmneval();
    const spanId = omneval.startSpan("test.span");
    expect(spanId).toBe("");
    expect(warnSpy).toHaveBeenCalledWith(
      "@omneval/sdk: Omneval.init() not called — startSpan() is a no-op"
    );
    warnSpy.mockRestore();
  });

  it("endSpan before init is a no-op", async () => {
    const omneval = createOmneval();
    await expect(omneval.endSpan("any-id")).resolves.toBeUndefined();
  });

  it("writeScore throws before init", async () => {
    const omneval = createOmneval();
    await expect(
      omneval.writeScore("span-1", { name: "eval", value: 1.0 })
    ).rejects.toThrow("Omneval.init() not called");
  });

  it("getPrompt throws before init", async () => {
    const omneval = createOmneval();
    await expect(omneval.getPrompt("test")).rejects.toThrow("Omneval.init() not called");
  });
});
