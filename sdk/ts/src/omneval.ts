import { OmnevalConfig, SpanAttributes, SpanKind } from "./types";
import { ManualTracer } from "./tracer";
import { SpanExporter } from "./exporter";
import { OmnevalClient } from "./client";

/**
 * Tracer and prompt client for Omneval.
 * For tests, use createOmneval() to get a fresh instance.
 */
export class OmnevalSDK {
  config?: OmnevalConfig;
  tracer?: ManualTracer;
  client?: OmnevalClient;
  exporter?: SpanExporter;

  init(config: OmnevalConfig): void {
    this.config = config;

    this.exporter = new SpanExporter(config.baseUrl, config.apiKey);
    this.tracer = new ManualTracer(this.exporter);
    this.tracer.init();

    this.client = new OmnevalClient(config);
  }

  startSpan(
    name: string,
    attributes?: SpanAttributes,
    kind?: SpanKind,
    parentSpanId?: string
  ): string {
    if (!this.tracer) {
      console.warn("@omneval/sdk: Omneval.init() not called — startSpan() is a no-op");
      return "";
    }

    return this.tracer.startSpan(name, {
      parentSpanId,
      kind,
      attributes,
    });
  }

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

  setModel(spanId: string, model: string): void {
    this.tracer?.setModel(spanId, model);
  }

  setInput(spanId: string, input: string): void {
    this.tracer?.setInput(spanId, input);
  }

  setTokens(spanId: string, inputTokens: number, outputTokens: number): void {
    this.tracer?.setTokens(spanId, inputTokens, outputTokens);
  }

  setPrompt(spanId: string, name: string, version?: number): void {
    this.tracer?.setPrompt(spanId, name, version);
  }

  async getPrompt(
    name: string,
    labelOrOptions?: string | { label?: string; version?: number }
  ): Promise<string> {
    if (!this.client) {
      throw new Error("@omneval/sdk: Omneval.init() not called");
    }

    if (typeof labelOrOptions === "string") {
      return this.client.getPrompt(name, { label: labelOrOptions });
    }

    return this.client.getPrompt(name, labelOrOptions);
  }

  async getPromptByVersion(name: string, version: number): Promise<string> {
    if (!this.client) {
      throw new Error("@omneval/sdk: Omneval.init() not called");
    }

    return this.client.getPromptByVersion(name, version);
  }

  async writeScore(
    spanId: string,
    evalName: string | { name: string; value: number; reasoning?: string },
    value?: number,
    reasoning?: string
  ): Promise<void> {
    if (!this.client) {
      throw new Error("@omneval/sdk: Omneval.init() not called");
    }

    if (typeof evalName === "string") {
      if (value === undefined) {
        throw new Error("value is required");
      }
      return this.client.writeScore(spanId, { name: evalName, value, reasoning });
    }

    return this.client.writeScore(spanId, evalName);
  }

  async flush(): Promise<void> {
    await this.tracer?.flush();
  }
}

export function createOmneval(): OmnevalSDK {
  return new OmnevalSDK();
}

export const Omneval = new OmnevalSDK();
