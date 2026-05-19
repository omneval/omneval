/**
 * @omneval/sdk/node — Node.js tracer with OTel auto-instrumentation.
 *
 * This is a conditional export of @omneval/sdk — it is only loaded when
 * explicitly imported via `import { instrument } from "@omneval/sdk/node"`.
 *
 * Usage:
 *   import { instrument } from "@omneval/sdk/node";
 *   instrument({ baseUrl: "http://localhost:3000", apiKey: "oev_proj_...", serviceName: "my-app" });
 *
 * This configures an OpenTelemetry NodeTracerProvider with an OTLP exporter
 * pointing at the Omneval ingest API, enabling zero-change tracing for any
 * framework that uses @opentelemetry/api (OpenAI, Express, etc.).
 *
 * Do NOT import this module in browser code — it is Node.js only.
 */

import { buildOmnevalExporterConfig } from "./exporter";

/**
 * OpenTelemetry SDK factory — allows tests to inject mock OTel classes.
 */
interface OTelFactory {
  NodeSDK: new (config: any) => {
    start(): void;
    shutdown(): Promise<void>;
  };
  OTLPTraceExporter: new (config: any) => any;
}

/**
 * Dynamically load OTel SDK packages.
 * Throws a descriptive error if not installed.
 */
async function loadOTelFactory(): Promise<OTelFactory> {
  try {
    const otelSdk = await import("@opentelemetry/sdk-node");
    const otelExporter = await import("@opentelemetry/exporter-trace-otlp-http");
    return {
      NodeSDK: otelSdk.NodeSDK,
      OTLPTraceExporter: otelExporter.OTLPTraceExporter,
    };
  } catch (err: unknown) {
    if (
      err instanceof Error &&
      err.message.includes("Cannot find module")
    ) {
      throw new Error(
        "@omneval/sdk/node: @opentelemetry/sdk-node is not installed. " +
          "Install it with: npm install @opentelemetry/sdk-node @opentelemetry/exporter-trace-otlp-http"
      );
    }
    throw err;
  }
}

// Module-level factory — can be overridden in tests
let _factory: OTelFactory | null = null;

export function _setFactory(factory: OTelFactory): void {
  _factory = factory;
}

export function _resetFactory(): void {
  _factory = null;
}

/**
 * Internal implementation of instrument with a factory parameter.
 */
function instrumentWithFactory(
  options: InstrumentOptions,
  factoryOverride?: OTelFactory
): ShutdownFn {
  const { baseUrl, apiKey, serviceName } = options;

  if (!baseUrl) {
    throw new Error("instrument: baseUrl is required");
  }

  // Use injected factory or load from npm
  const factory = factoryOverride || _factory;
  if (!factory) {
    throw new Error(
      "@omneval/sdk/node: OTel factory not available. " +
        "This should not happen in production. " +
        "If you see this error, please report it at https://github.com/omneval/omneval/issues"
    );
  }

  // Build the OTLP exporter config for Omneval
  const exporterConfig = buildOmnevalExporterConfig({ baseUrl, apiKey });

  // Build the NodeSDK configuration
  const config: any = {
    traceExporter: new factory.OTLPTraceExporter(exporterConfig),
  };

  if (serviceName) {
    config.resource = {
      attributes: {
        "service.name": serviceName,
      },
    };
  }

  const nodeSDK = new factory.NodeSDK(config);
  nodeSDK.start();

  return async () => {
    await nodeSDK.shutdown();
  };
}

export interface InstrumentOptions {
  /** Base URL of the Omneval ingest API (e.g. http://localhost:3000). */
  baseUrl: string;
  /** API key for authentication. */
  apiKey?: string;
  /** Service name to tag all spans with (used as service.name resource attribute). */
  serviceName?: string;
}

/**
 * Stop the tracer and flush remaining spans.
 * Call this before process.exit() to ensure all spans are exported.
 */
export type ShutdownFn = () => Promise<void>;

/**
 * Configure OpenTelemetry in Node.js to export traces to Omneval.
 *
 * This function:
 * 1. Imports @opentelemetry/sdk-node and @opentelemetry/exporter-trace-otlp-http
 * 2. Creates a NodeSDK instance with an OTLP trace exporter
 * 3. Points the exporter at {baseUrl}/v1/traces with Bearer token auth
 * 4. Starts the SDK (which registers the global TracerProvider)
 * 5. Returns a shutdown function for graceful cleanup
 *
 * @param options - Configuration options
 * @returns A shutdown function
 * @throws If @opentelemetry/sdk-node is not installed, or if configuration is invalid
 *
 * @example
 *   import { instrument } from "@omneval/sdk/node";
 *   const shutdown = instrument({
 *     baseUrl: "http://localhost:3000",
 *     apiKey: "oev_proj_abc123",
 *     serviceName: "my-llm-app",
 *   });
 *
 *   // ... your app code, using OpenAI with auto-instrumentation ...
 *
 *   // On shutdown:
 *   await shutdown();
 *   process.exit(0);
 */
export function instrument(options: InstrumentOptions): ShutdownFn {
  return instrumentWithFactory(options);
}
