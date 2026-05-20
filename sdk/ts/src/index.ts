/**
 * @omneval/sdk — Browser-compatible tracer and prompt client for Omneval.
 *
 * Usage:
 *   import { Omneval } from "@omneval/sdk";
 *   Omneval.init({ baseUrl: "http://localhost:3000", apiKey: "oev_proj_..." });
 *
 *   const spanId = Omneval.startSpan("llm.call", { model: "gpt-4" });
 *   Omneval.setModel(spanId, "gpt-4");
 *   Omneval.setInput(spanId, "Hello!");
 *   await Omneval.endSpan(spanId, { output: "Hi there!" });
 */

export { Omneval, createOmneval, OmnevalSDK } from "./omneval";
export type {
  OmnevalConfig,
  OmnevalSpan,
  SpanAttributes,
  SpanKind,
  PromptVersion,
  PromptModelConfig,
  GetPromptOptions,
  WriteScoreOptions,
} from "./types";
