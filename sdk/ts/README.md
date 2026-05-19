# @omneval/sdk

Browser-compatible OpenTelemetry tracer and prompt client for [Omneval](https://github.com/omneval/omneval).

Send spans, write scores, and fetch prompts — all using the native Fetch API. No Node.js dependencies required.

For Node.js, use `@omneval/sdk/node` for automatic OpenTelemetry instrumentation of LLM frameworks.

## Quickstart

```bash
npm install @omneval/sdk
```

```ts
import { Omneval } from "@omneval/sdk";

// Initialize with your Omneval Query API URL
Omneval.init({
  baseUrl: "http://localhost:3000",
  apiKey: "oev_proj_your_api_key",
});

// Start a span
const spanId = Omneval.startSpan("llm.call", { kind: "llm" });

// Set span attributes
Omneval.setModel(spanId, "gpt-4");
Omneval.setInput(spanId, "Hello!");
Omneval.setTokens(spanId, 10, 5);

// End the span with output
await Omneval.endSpan(spanId, "Hi there!");

// Fetch a prompt (cached for 30 seconds)
const template = await Omneval.getPrompt("greeting", "production");

// Write a manual score
await Omneval.writeScore(spanId, {
  name: "helpfulness",
  value: 0.8,
  reasoning: "Great answer",
});
```

## API Reference

### `Omneval.init(config)`

Initializes the SDK. Call once at application startup.

```ts
Omneval.init({
  baseUrl: "http://localhost:3000",
  apiKey?: "oev_proj_...",
});
```

### `Omneval.startSpan(name, attributes?, kind?, parentSpanId?)`

Starts a new span. Returns a span ID to use with `endSpan()`.

```ts
const spanId = Omneval.startSpan("llm.call", { kind: "llm" });
```

### `Omneval.endSpan(spanId, output?)`

Ends a span and exports all pending spans.

```ts
// String output
await Omneval.endSpan(spanId, "response text");

// Object with output and attributes
await Omneval.endSpan(spanId, {
  output: "response text",
  attributes: { custom: "value" },
});
```

### `Omneval.setModel(spanId, model)`

Sets the model name on an active span.

```ts
Omneval.setModel(spanId, "gpt-4");
```

### `Omneval.setInput(spanId, input)`

Sets the input on an active span.

```ts
Omneval.setInput(spanId, "Hello!");
```

### `Omneval.setTokens(spanId, inputTokens, outputTokens)`

Sets token counts on an active span.

```ts
Omneval.setTokens(spanId, 100, 50);
```

### `Omneval.setPrompt(spanId, name, version?)`

Sets the prompt name and optional version on an active span.

```ts
Omneval.setPrompt(spanId, "greeting", 1);
```

### `Omneval.getPrompt(name, options?)`

Fetches a prompt by name and label (defaults to "production"). Cached client-side for 30 seconds.

```ts
const template = await Omneval.getPrompt("greeting", "production");

// Or with options object
const template = await Omneval.getPrompt("greeting", { label: "staging" });

// Or by version (immutable cache, no TTL)
const template = await Omneval.getPrompt("greeting", { version: 2 });
```

### `Omneval.writeScore(spanId, options)`

Writes a manual score for a span. Generates a trace ID automatically.

```ts
// Object syntax
await Omneval.writeScore(spanId, {
  name: "accuracy",
  value: 0.95,
  reasoning: "Perfect answer",
});

// Shorthand: writeScore(spanId, evalName, value, reasoning?)
await Omneval.writeScore(spanId, "helpfulness", 0.8);
```

### `Omneval.flush()`

Forces export of all pending spans.

```ts
await Omneval.flush();
```

## Node.js — Automatic Tracing with OpenTelemetry

For Node.js applications, use the `@omneval/sdk/node` entry point to configure automatic tracing of LLM frameworks (OpenAI, LangChain, etc.) via OpenTelemetry auto-instrumentation.

```bash
npm install @omneval/sdk
npm install @opentelemetry/sdk-node @opentelemetry/exporter-trace-otlp-http
npm install @opentelemetry/instrumentation-openai
```

```ts
import { instrument } from "@omneval/sdk/node";

// Configure the OTel tracer to export traces to Omneval
const shutdown = instrument({
  baseUrl: "http://localhost:3000",
  apiKey: "oev_proj_your_api_key",
  serviceName: "my-llm-app",
});

// Now any OpenTelemetry-compatible auto-instrumentation works
import { OpenAIInstrumentation } from "@opentelemetry/instrumentation-openai";
// ... configure and use auto-instrumentation ...

// On shutdown, flush remaining spans and stop
await shutdown();
process.exit(0);
```

### `instrument(options)`

Configures OpenTelemetry in Node.js to export traces to Omneval.

```ts
const shutdown = instrument({
  baseUrl: "http://localhost:3000",  // Required
  apiKey?: "oev_proj_...",            // Optional — sent as Authorization: Bearer
  serviceName?: "my-app",             // Optional — service.name resource attribute
});
```

**What it does:**
1. Imports `@opentelemetry/sdk-node` and `@opentelemetry/exporter-trace-otlp-http`
2. Creates a `NodeSDK` instance with an OTLP trace exporter
3. Points the exporter at `{baseUrl}/v1/traces` with `Authorization: Bearer {apiKey}`
4. Starts the SDK (which registers the global `TracerProvider`)
5. Returns a `shutdown` function for graceful cleanup

**Requirements:**
- Node.js 18 or later
- `@opentelemetry/sdk-node` and `@opentelemetry/exporter-trace-otlp-http` must be installed

## Browser Compatibility

This SDK uses only browser-native APIs:

- `fetch` for HTTP requests
- `crypto.getRandomValues` for ID generation
- No Node.js APIs (`process`, `fs`, `net`, etc.)

Works in all modern browsers and edge runtimes (Cloudflare Workers, Deno, etc.).

## License

MIT
