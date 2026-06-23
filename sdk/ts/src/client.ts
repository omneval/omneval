import {
  CreateEvalRuleOptions,
  CreatePromptOptions,
  EvalRule,
  GetPromptOptions,
  OmnevalConfig,
  PromptListItem,
  PromptVersion,
  WriteScoreOptions,
} from "./types";
import { generateTraceId } from "./id";

const PROMPT_CACHE_TTL_MS = 30_000;

/**
 * Extract the project identifier from an API key.
 * Mirrors Python SDK's _extract_project_id.
 *   oev_proj_<suffix> → <suffix>
 *   oev_svc_<suffix>  → <suffix>
 *   anything else     → the key itself (or "" if undefined)
 */
function extractProjectId(apiKey?: string): string {
  if (!apiKey) return "";
  if (apiKey.startsWith("oev_proj_")) return apiKey.slice("oev_proj_".length);
  if (apiKey.startsWith("oev_svc_")) return apiKey.slice("oev_svc_".length);
  return apiKey;
}

export class OmnevalClient {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly projectId: string;

  private readonly labelCache = new Map<
    string,
    { template: string; expiresAt: number }
  >();
  private readonly versionCache = new Map<string, string>();

  constructor(config: OmnevalConfig) {
    this.baseUrl = config.baseUrl;
    this.apiKey = config.apiKey;
    this.projectId = extractProjectId(config.apiKey);
  }

  /**
   * Fetch a prompt by name and label (defaults to "production").
   * Returns the template string.
   */
  async getPrompt(
    name: string,
    options?: GetPromptOptions
  ): Promise<string> {
    const { label = "production", version } = options ?? {};

    // If explicit version is provided, use version cache / endpoint
    if (version !== undefined && version > 0) {
      return this.getPromptByVersion(name, version);
    }

    // Use label cache
    const cacheKey = `${name}|${label}`;
    const cached = this.labelCache.get(cacheKey);
    if (cached && Date.now() < cached.expiresAt) {
      return cached.template;
    }

    // Cache miss — fetch from server
    const params = new URLSearchParams({ label });
    const pv = await this.fetchPromptVersionFromServer(name, params);
    const template = pv.template;

    // Store in label cache with TTL
    this.labelCache.set(cacheKey, {
      template,
      expiresAt: Date.now() + PROMPT_CACHE_TTL_MS,
    });

    return template;
  }

  /**
   * Fetch a prompt by explicit version number (immutable cache, no TTL).
   */
  async getPromptByVersion(name: string, version: number): Promise<string> {
    const cacheKey = `${name}|${version}`;

    const cached = this.versionCache.get(cacheKey);
    if (cached !== undefined) {
      return cached;
    }

    const pv = await this.fetchPromptByVersionFromServer(name, version);
    this.versionCache.set(cacheKey, pv.template);

    return pv.template;
  }

  /**
   * Write a manual score for a span.
   * Generates a trace_id automatically.
   */
  async writeScore(
    spanId: string,
    options: WriteScoreOptions
  ): Promise<void> {
    if (!spanId) {
      throw new Error("span_id is required");
    }

    const score = {
      span_id: spanId,
      trace_id: generateTraceId(),
      project_id: this.projectId,
      eval_name: options.name,
      value: options.value,
      reasoning: options.reasoning,
    };

    const url = `${this.baseUrl}/api/v1/scores`;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify(score),
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`write score: ${response.status}: ${body}`);
    }
  }

  /**
   * Create a new prompt version.
   * Returns the created prompt version data.
   */
  async createPrompt(
    name: string,
    template: string,
    options?: CreatePromptOptions
  ): Promise<PromptVersion> {
    if (!name) {
      throw new Error("name is required");
    }
    if (!template) {
      throw new Error("template is required");
    }

    const payload: Record<string, unknown> = { name, template };
    if (options?.model_config) {
      payload.model_config = options.model_config;
    }
    if (options?.label) {
      payload.label = options.label;
    }

    const url = `${this.baseUrl}/api/v1/prompts`;
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`create prompt: ${response.status}: ${body}`);
    }

    return (await response.json()) as PromptVersion;
  }

  /**
   * List all prompts for the project.
   * Returns an array of prompt summary items.
   */
  async listPrompts(): Promise<PromptListItem[]> {
    const url = `${this.baseUrl}/api/v1/prompts`;
    const headers: Record<string, string> = {};
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, { headers });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`list prompts: ${response.status}: ${body}`);
    }

    return (await response.json()) as PromptListItem[];
  }

  /**
   * Assign a label to a specific prompt version.
   */
  async setPromptLabel(name: string, label: string, version: number): Promise<void> {
    if (!name) {
      throw new Error("name is required");
    }

    const url = `${this.baseUrl}/api/v1/prompts/${name}/labels/${label}`;
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, {
      method: "PUT",
      headers,
      body: JSON.stringify({ version }),
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`set prompt label: ${response.status}: ${body}`);
    }
  }

  /**
   * List all eval rules for the project.
   * Returns an array of EvalRule objects.
   */
  async listEvalRules(): Promise<EvalRule[]> {
    const url = `${this.baseUrl}/api/v1/eval-rules`;
    const headers: Record<string, string> = {};
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, { headers });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`list eval rules: ${response.status}: ${body}`);
    }

    const data = (await response.json()) as { rules: EvalRule[] };
    return data.rules ?? [];
  }

  /**
   * Create an eval rule.
   * Returns the created eval rule data.
   */
  async createEvalRule(
    name: string,
    promptName: string,
    options?: CreateEvalRuleOptions
  ): Promise<EvalRule> {
    if (!name) {
      throw new Error("name is required");
    }
    if (!promptName) {
      throw new Error("prompt_name is required");
    }

    const payload: Record<string, unknown> = {
      name,
      prompt_name: promptName,
      filter: options?.filter ?? {},
      sample_rate: options?.sample_rate ?? 1.0,
    };
    if (options?.judge_model) {
      payload.judge_model = options.judge_model;
    }
    if (options?.prompt_version != null) {
      payload.prompt_version = options.prompt_version;
    }
    if (options?.prompt_label != null) {
      payload.prompt_label = options.prompt_label;
    }

    const url = `${this.baseUrl}/api/v1/eval-rules`;
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`create eval rule: ${response.status}: ${body}`);
    }

    return (await response.json()) as EvalRule;
  }

  // ---- Private helpers ----

  private async fetchPromptVersionFromServer(
    name: string,
    params: URLSearchParams
  ): Promise<PromptVersion> {
    const url = `${this.baseUrl}/api/v1/prompts/${name}?${params}`;

    const response = await fetch(url);

    if (response.status === 404) {
      throw new Error(`prompt not found: ${name}`);
    }

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`get prompt: ${response.status}: ${body}`);
    }

    return (await response.json()) as PromptVersion;
  }

  private async fetchPromptByVersionFromServer(
    name: string,
    version: number
  ): Promise<PromptVersion> {
    const params = new URLSearchParams({ version: String(version) });
    return this.fetchPromptVersionFromServer(name, params);
  }
}


