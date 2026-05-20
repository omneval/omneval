import { OmnevalSpan } from "./types";

export class SpanExporter {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly timeoutMs = 10_000; // 10 seconds

  constructor(baseUrl: string, apiKey?: string) {
    this.baseUrl = baseUrl;
    this.apiKey = apiKey;
  }

  async export(spans: OmnevalSpan[]): Promise<boolean> {
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
      return false;
    }
  }
}
