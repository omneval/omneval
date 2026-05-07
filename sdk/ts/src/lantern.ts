import { LanternConfig, SpanAttributes, SpanKind } from "./types";
import { ManualTracer } from "./tracer";
import { SpanExporter } from "./exporter";
import { LanternClient } from "./client";

/**
 * LanternSDK is a tracer and prompt client for Lantern.
 * Browser-compatible — uses only native Fetch API, no Node.js APIs.
 *
 * For tests, use `createLantern()` to get a fresh instance.
 * For production, use the singleton `Lantern`.
 */
export class LanternSDK {
  config?: LanternConfig;
  tracer?: ManualTracer;
  client?: LanternClient;
  exporter?: SpanExporter;

  /**
   * Initialize the SDK with baseUrl and optional apiKey.
   * Call once at application startup before any tracing.
   */
  init(config: LanternConfig): void {
    this.config = config;

    this.exporter = new SpanExporter(config.baseUrl, config.apiKey);
    this.tracer = new ManualTracer(this.exporter);
    this.tracer.init();

    this.client = new LanternClient(config);
  }

  /**
   * Start a new span. Returns a span ID to use with endSpan().
   */
  startSpan(
    name: string,
    attributes?: SpanAttributes,
    kind?: SpanKind,
    parentSpanId?: string
  ): string {
    if (!this.tracer) {
      console.warn(
        "@lantern/sdk: Lantern.init() not called — startSpan() is a no-op"
      );
      return "";
    }

    return this.tracer.startSpan(name, {
      parentSpanId,
      kind,
      attributes,
    });
  }

  /**
   * End a span by ID, optionally attaching output.
   */
  async endSpan(
    spanId: string,
    output?: string | { output?: string; attributes?: SpanAttributes }
  ): Promise<void> {
    if (!this.tracer) {
      return;
    }

    let outputStr: string | undefined;
    let extraAttrs: SpanAttributes | undefined;

    if (typeof output === "string") {
      outputStr = output;
    } else if (output) {
      outputStr = output.output;
      extraAttrs = output.attributes;
    }

    await this.tracer.endSpan(spanId, { output: outputStr, attributes: extraAttrs });
  }

  /**
   * Set the model name on an active span.
   */
  setModel(spanId: string, model: string): void {
    this.tracer?.setModel(spanId, model);
  }

  /**
   * Set the input on an active span.
   */
  setInput(spanId: string, input: string): void {
    this.tracer?.setInput(spanId, input);
  }

  /**
   * Set token counts on an active span.
   */
  setTokens(spanId: string, inputTokens: number, outputTokens: number): void {
    this.tracer?.setTokens(spanId, inputTokens, outputTokens);
  }

  /**
   * Set the prompt name/version on an active span.
   */
  setPrompt(spanId: string, name: string, version?: number): void {
    this.tracer?.setPrompt(spanId, name, version);
  }

  /**
   * Fetch a prompt by name and label (defaults to "production").
   * Cached client-side for 30 seconds.
   */
  async getPrompt(
    name: string,
    labelOrOptions?: string | { label?: string; version?: number }
  ): Promise<string> {
    if (!this.client) {
      throw new Error("@lantern/sdk: Lantern.init() not called");
    }

    if (typeof labelOrOptions === "string") {
      return this.client.getPrompt(name, { label: labelOrOptions });
    }

    return this.client.getPrompt(name, labelOrOptions);
  }

  /**
   * Fetch a prompt by explicit version number.
   */
  async getPromptByVersion(name: string, version: number): Promise<string> {
    if (!this.client) {
      throw new Error("@lantern/sdk: Lantern.init() not called");
    }

    return this.client.getPromptByVersion(name, version);
  }

  /**
   * Write a manual score for a span.
   */
  async writeScore(
    spanId: string,
    evalName: string | { name: string; value: number; reasoning?: string },
    value?: number,
    reasoning?: string
  ): Promise<void> {
    if (!this.client) {
      throw new Error("@lantern/sdk: Lantern.init() not called");
    }

    if (typeof evalName === "string") {
      if (value === undefined) {
        throw new Error("value is required");
      }
      return this.client.writeScore(spanId, { name: evalName, value, reasoning });
    }

    return this.client.writeScore(spanId, evalName);
  }

  /**
   * Export pending spans to the ingest API.
   */
  async flush(): Promise<void> {
    await this.tracer?.flush();
  }
}

/**
 * Create a fresh LanternSDK instance (useful for tests).
 */
export function createLantern(): LanternSDK {
  return new LanternSDK();
}

// Singleton instance for production use
export const Lantern = new LanternSDK();
