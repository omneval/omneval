import { GetPromptOptions, OmnevalConfig, PromptVersion, WriteScoreOptions } from "./types";
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


