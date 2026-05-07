/**
 * @lantern/sdk — Browser-compatible tracer and prompt client for Lantern.
 *
 * Usage:
 *   import { Lantern } from "@lantern/sdk";
 *   Lantern.init({ baseUrl: "http://localhost:3000", apiKey: "ltn_proj_..." });
 *
 *   const spanId = Lantern.startSpan("llm.call", { model: "gpt-4" });
 *   Lantern.setModel(spanId, "gpt-4");
 *   Lantern.setInput(spanId, "Hello!");
 *   await Lantern.endSpan(spanId, { output: "Hi there!" });
 */

export { Lantern, createLantern, LanternSDK } from "./lantern";
export type {
  LanternConfig,
  LanternSpan,
  SpanAttributes,
  SpanKind,
  PromptVersion,
  PromptModelConfig,
  GetPromptOptions,
  WriteScoreOptions,
} from "./types";
