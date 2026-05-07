import {
  GetPromptOptions,
  LanternConfig,
  LanternSpan,
  PromptVersion,
  WriteScoreOptions,
} from "./types";

const PROMPT_CACHE_TTL_MS = 30_000; // 30 seconds, matching snapshot staleness window

/**
 * LanternClient handles prompt fetch with caching and manual score writes.
 * Browser-compatible — uses the Fetch API exclusively.
 */
export class LanternClient {
  private readonly baseUrl: string;
  private readonly apiKey?: string;

  // Prompt caches
  // label cache: key = name + "|" + label, value = { template, expiresAt }
  private readonly labelCache = new Map<
    string,
    { template: string; expiresAt: number }
  >();
  // version cache: key = name + "|" + version, value = template (no TTL)
  private readonly versionCache = new Map<string, string>();

  constructor(config: LanternConfig) {
    this.baseUrl = config.baseUrl;
    this.apiKey = config.apiKey;
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
    const pv = await this.fetchPromptFromServer(name, label);
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

    const traceId = this.generateTraceId();

    const score = {
      span_id: spanId,
      trace_id: traceId,
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
   * Export a batch of spans via the Lantern ingest API.
   */
  async exportSpans(spans: LanternSpan[]): Promise<boolean> {
    if (spans.length === 0) {
      return true;
    }

    const url = `${this.baseUrl}/api/v1/spans`;
    const body = JSON.stringify({ spans });

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.apiKey) {
      headers["X-API-Key"] = this.apiKey;
    }

    try {
      const response = await fetch(url, {
        method: "POST",
        headers,
        body,
      });

      return response.ok;
    } catch {
      return false;
    }
  }

  // ---- Private helpers ----

  private async fetchPromptFromServer(
    name: string,
    label: string
  ): Promise<PromptVersion> {
    const url = `${this.baseUrl}/api/v1/prompts/${name}?label=${encodeURIComponent(label)}`;

    const response = await fetch(url);

    if (response.status === 404) {
      throw new Error(`prompt not found: ${name}`);
    }

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`get prompt: ${response.status}: ${body}`);
    }

    const pv = (await response.json()) as PromptVersion;
    return pv;
  }

  private async fetchPromptByVersionFromServer(
    name: string,
    version: number
  ): Promise<PromptVersion> {
    const url = `${this.baseUrl}/api/v1/prompts/${name}?version=${version}`;

    const response = await fetch(url);

    if (response.status === 404) {
      throw new Error(`prompt not found: ${name}`);
    }

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`get prompt: ${response.status}: ${body}`);
    }

    const pv = (await response.json()) as PromptVersion;
    return pv;
  }

  private generateTraceId(): string {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }
}
