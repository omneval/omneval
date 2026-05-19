import { generateSpanId, generateTraceId } from "./id";
import { SpanExporter } from "./exporter";
import { OmnevalSpan, SpanAttributes, SpanKind } from "./types";

interface TrackedSpan {
  span_id: string;
  trace_id: string;
  parent_id?: string;
  name: string;
  kind?: SpanKind;
  model?: string;
  input?: string;
  output?: string;
  input_tokens?: number;
  output_tokens?: number;
  prompt_name?: string;
  prompt_version?: number;
  attributes?: Record<string, string | number | boolean>;
  start_time: number;
  end_time?: number;
}

/**
 * Manual tracer that tracks spans in-memory and exports them via SpanExporter.
 */
export class ManualTracer {
  private readonly exporter: SpanExporter;
  private pending: TrackedSpan[] = [];

  constructor(exporter: SpanExporter) {
    this.exporter = exporter;
  }

  init(): void {}

  startSpan(
    name: string,
    options?: {
      parentSpanId?: string;
      kind?: SpanKind;
      attributes?: SpanAttributes;
    }
  ): string {
    const spanId = generateSpanId();
    const traceId = options?.parentSpanId
      ? this.findTraceId(options.parentSpanId)
      : generateTraceId();

    const tracked: TrackedSpan = {
      span_id: spanId,
      trace_id: traceId,
      parent_id: options?.parentSpanId,
      name,
      kind: options?.kind,
      attributes: options?.attributes,
      start_time: Date.now(),
    };

    this.pending.push(tracked);
    return spanId;
  }

  async endSpan(
    spanId: string,
    options?: {
      output?: string;
      attributes?: SpanAttributes;
    }
  ): Promise<void> {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (!tracked) {
      return;
    }

    tracked.end_time = Date.now();
    tracked.output = options?.output;
    if (options?.attributes) {
      tracked.attributes = { ...tracked.attributes, ...options.attributes };
    }

    await this.flush();
  }

  async flush(): Promise<void> {
    if (this.pending.length === 0) {
      return;
    }

    const spans: OmnevalSpan[] = this.pending.map((s) => ({
      span_id: s.span_id,
      trace_id: s.trace_id,
      parent_id: s.parent_id,
      name: s.name,
      kind: s.kind,
      model: s.model,
      input: s.input,
      output: s.output,
      input_tokens: s.input_tokens,
      output_tokens: s.output_tokens,
      prompt_name: s.prompt_name,
      prompt_version: s.prompt_version,
      attributes: s.attributes,
      start_time: s.start_time,
      end_time: s.end_time ?? Date.now(),
    }));

    const success = await this.exporter.export(spans);

    if (success) {
      this.pending = [];
    }
  }

  setModel(spanId: string, model: string): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.model = model;
    }
  }

  setInput(spanId: string, input: string): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.input = input;
    }
  }

  setTokens(spanId: string, inputTokens: number, outputTokens: number): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.input_tokens = inputTokens;
      tracked.output_tokens = outputTokens;
    }
  }

  setPrompt(
    spanId: string,
    name: string,
    version?: number
  ): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.prompt_name = name;
      if (version !== undefined) {
        tracked.prompt_version = version;
      }
    }
  }

  private findTraceId(spanId: string): string {
    let currentSpanId = spanId;

    for (let iteration = 0; iteration < this.pending.length; iteration++) {
      const tracked = this.pending.find((s) => s.span_id === currentSpanId);
      if (!tracked) {
        return generateTraceId();
      }
      if (!tracked.parent_id) {
        return tracked.trace_id;
      }
      currentSpanId = tracked.parent_id;
    }

    // Safety: if we exhaust all pending spans without finding a root,
    // generate a new trace ID to avoid infinite loops.
    return generateTraceId();
  }
}
