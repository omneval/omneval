// Types for the Lantern TypeScript SDK

/** Span kind classification. */
export type SpanKind =
  | "llm"
  | "tool"
  | "agent"
  | "chain"
  | "internal";

/** Attributes attached to a span. */
export type SpanAttributes = Record<string, string | number | boolean>;

/** A span ready to be exported to the Lantern ingest API. */
export interface LanternSpan {
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
  start_time?: number; // epoch milliseconds
  end_time?: number;   // epoch milliseconds
}

/** Configuration for Lantern.init(). */
export interface LanternConfig {
  /** Base URL of the Lantern Query API (e.g. http://localhost:3000). */
  baseUrl: string;
  /** API key for authentication (sent as X-API-Key header). */
  apiKey?: string;
}

/** Prompt model configuration. */
export interface PromptModelConfig {
  model?: string;
  temperature?: number;
  max_tokens?: number;
}

/** Prompt version returned by the API. */
export interface PromptVersion {
  name: string;
  version: number;
  template: string;
  model_config?: PromptModelConfig;
}

/** Options for getPrompt(). */
export interface GetPromptOptions {
  /** Label to resolve (e.g. "production", "staging", "dev"). Defaults to "production". */
  label?: string;
  /** Version number to fetch directly (mutually exclusive with label). */
  version?: number;
}

/** Options for writeScore(). */
export interface WriteScoreOptions {
  /** Evaluation name (e.g. "helpfulness", "accuracy"). */
  name: string;
  /** Score value (0.0 – 1.0). */
  value: number;
  /** Optional reasoning for the score. */
  reasoning?: string;
}
