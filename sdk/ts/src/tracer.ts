import { generateSpanId, generateTraceId } from "./id";
import { SpanExporter } from "./exporter";
import { LanternSpan, SpanAttributes, SpanKind } from "./types";

/**
 * A single span instance tracked in the manual tracer.
 */
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
 * Manual tracer that tracks spans in-memory and exports them via the SpanExporter.
 * Browser-compatible — uses no Node.js APIs.
 */
export class ManualTracer {
  private readonly exporter: SpanExporter;
  private pending: TrackedSpan[] = [];
  private initialized = false;

  constructor(exporter: SpanExporter) {
    this.exporter = exporter;
  }

  /**
   * Initialize the tracer with a fresh span ID generator.
   * Call once after Lantern.init() to set up the exporter.
   */
  init(): void {
    this.initialized = true;
  }

  /**
   * Start a new span. Returns a span ID that must be passed to endSpan().
   * Spans are exported synchronously (no batching).
   */
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

  /**
   * End a span by ID, optionally attaching output.
   * All pending spans are exported after ending.
   */
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

    if (options?.output !== undefined) {
      tracked.output = options.output;
    }

    if (options?.attributes) {
      tracked.attributes = { ...tracked.attributes, ...options.attributes };
    }

    await this.flush();
  }

  /**
   * Flush all pending spans to the ingest API.
   * Spans are sent as a single batch.
   */
  async flush(): Promise<void> {
    if (this.pending.length === 0) {
      return;
    }

    const spans: LanternSpan[] = this.pending.map((s) => ({
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
    } else {
      // On failure, keep spans in pending for retry
      // In a production SDK, this would use exponential backoff
    }
  }

  /**
   * Set the model name on the most recently started (not yet ended) span.
   */
  setModel(spanId: string, model: string): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.model = model;
    }
  }

  /**
   * Set the input on the most recently started (not yet ended) span.
   */
  setInput(spanId: string, input: string): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.input = input;
    }
  }

  /**
   * Set token counts on the most recently started (not yet ended) span.
   */
  setTokens(spanId: string, inputTokens: number, outputTokens: number): void {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked) {
      tracked.input_tokens = inputTokens;
      tracked.output_tokens = outputTokens;
    }
  }

  /**
   * Set the prompt name/version on the most recently started span.
   */
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

  /**
   * Find the trace ID for a span (walks up parent chain).
   */
  private findTraceId(spanId: string): string {
    const tracked = this.pending.find((s) => s.span_id === spanId);
    if (tracked?.parent_id) {
      return this.findTraceId(tracked.parent_id);
    }
    return tracked?.trace_id ?? generateTraceId();
  }
}
