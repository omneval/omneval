import { LanternSpan } from "./types";

/**
 * SpanExporter sends spans to the Lantern ingest API via the Fetch API.
 * Browser-compatible — uses no Node.js APIs.
 */
export class SpanExporter {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly timeoutMs = 10_000;

  constructor(baseUrl: string, apiKey?: string) {
    this.baseUrl = baseUrl;
    this.apiKey = apiKey;
  }

  /**
   * Export spans as a batch POST request.
   * Returns true if the request was accepted (2xx), false otherwise.
   */
  async export(spans: LanternSpan[]): Promise<boolean> {
    if (spans.length === 0) {
      return true;
    }

    const url = `${this.baseUrl}/api/v1/spans`;
    const body = JSON.stringify({ spans });

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), this.timeoutMs);

      const headers: Record<string, string> = {
        "Content-Type": "application/json",
      };
      if (this.apiKey) {
        headers["X-API-Key"] = this.apiKey;
      }

      const response = await fetch(url, {
        method: "POST",
        headers,
        body,
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        return false;
      }

      return true;
    } catch {
      // Network error or timeout — don't throw, just return false
      // (browser environments may not have a live server)
      return false;
    }
  }
}
