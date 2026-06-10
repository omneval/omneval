// Types for the Omneval TypeScript SDK

/** Span kind classification. */
export type SpanKind =
  | "llm"
  | "tool"
  | "agent"
  | "chain"
  | "internal";

/** Attributes attached to a span. */
export type SpanAttributes = Record<string, string | number | boolean>;

/** A span ready to be exported to the Omneval ingest API. */
export interface OmnevalSpan {
  span_id: string;
  trace_id: string;
  parent_id?: string;
  conversation_id?: string;
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

/** Configuration for Omneval.init(). */
export interface OmnevalConfig {
  /** Base URL of the Omneval Query API (e.g. http://localhost:3000). */
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

/** Options for createPrompt(). */
export interface CreatePromptOptions {
  /** Nested model configuration. Takes precedence over flat fields. */
  model_config?: PromptModelConfig;
  /** Optional label to assign at creation time (e.g. "production"). */
  label?: string;
}

/** A prompt list item returned by GET /api/v1/prompts. */
export interface PromptListItem {
  name: string;
  latest_version: number;
  labels: Record<string, number>;
}

/** An eval rule returned by the API. */
export interface EvalRule {
  rule_id: string;
  project_id?: string;
  name: string;
  judge_model: string;
  prompt_name: string;
  prompt_version: number;
  filter: Record<string, unknown>;
  sample_rate: number;
  enabled: boolean;
  created_at?: string;
}

/** Options for createEvalRule(). */
export interface CreateEvalRuleOptions {
  /** Filter conditions for matching spans. */
  filter?: Record<string, unknown>;
  /** Judge model name (defaults to server default). */
  judge_model?: string;
  /** Fraction of matching spans to evaluate (0.0–1.0). Defaults to 1.0. */
  sample_rate?: number;
}
