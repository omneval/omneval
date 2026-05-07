/**
 * Lantern-specific OTLP trace exporter configuration.
 *
 * Creates a standard OTLP HTTP exporter configured to send traces
 * to the Lantern ingest API at {baseUrl}/v1/traces with Bearer token auth.
 */

import type { OTLPExporterConfigBase } from "@opentelemetry/otlp-exporter-base";

export interface LanternExporterConfig {
  /** Base URL of the Lantern Query/Ingest API (e.g. http://localhost:3000). */
  baseUrl: string;
  /** API key for authentication (sent as Authorization: Bearer header). */
  apiKey?: string;
}

/**
 * Build an OTLP HTTP exporter config for the Lantern ingest endpoint.
 *
 * The exporter posts OTLP JSON to POST {baseUrl}/v1/traces with
 * Authorization: Bearer {apiKey} headers.
 *
 * @param config - Lantern connection configuration
 * @returns OTLP trace exporter configuration object
 */
export function buildLanternExporterConfig(config: LanternExporterConfig): Partial<OTLPExporterConfigBase> {
  const baseUrl = config.baseUrl.replace(/\/+$/, "");
  return {
    url: `${baseUrl}/v1/traces`,
    headers: config.apiKey
      ? { Authorization: `Bearer ${config.apiKey}` }
      : undefined,
  };
}
