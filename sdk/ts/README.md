# @lantern/sdk

Browser-compatible OpenTelemetry tracer and prompt client for [Lantern](https://github.com/zbloss/lantern).

Send spans, write scores, and fetch prompts — all using the native Fetch API. No Node.js dependencies required.

## Quickstart

```bash
npm install @lantern/sdk
```

```ts
import { Lantern } from "@lantern/sdk";

// Initialize with your Lantern Query API URL
Lantern.init({
  baseUrl: "http://localhost:3000",
  apiKey: "ltn_proj_your_api_key",
});

// Start a span
const spanId = Lantern.startSpan("llm.call", { kind: "llm" });

// Set span attributes
Lantern.setModel(spanId, "gpt-4");
Lantern.setInput(spanId, "Hello!");
Lantern.setTokens(spanId, 10, 5);

// End the span with output
await Lantern.endSpan(spanId, "Hi there!");

// Fetch a prompt (cached for 30 seconds)
const template = await Lantern.getPrompt("greeting", "production");

// Write a manual score
await Lantern.writeScore(spanId, {
  name: "helpfulness",
  value: 0.8,
  reasoning: "Great answer",
});
```

## API Reference

### `Lantern.init(config)`

Initializes the SDK. Call once at application startup.

```ts
Lantern.init({
  baseUrl: "http://localhost:3000",
  apiKey?: "ltn_proj_...",
});
```

### `Lantern.startSpan(name, attributes?, kind?, parentSpanId?)`

Starts a new span. Returns a span ID to use with `endSpan()`.

```ts
const spanId = Lantern.startSpan("llm.call", { kind: "llm" });
```

### `Lantern.endSpan(spanId, output?)`

Ends a span and exports all pending spans.

```ts
// String output
await Lantern.endSpan(spanId, "response text");

// Object with output and attributes
await Lantern.endSpan(spanId, {
  output: "response text",
  attributes: { custom: "value" },
});
```

### `Lantern.setModel(spanId, model)`

Sets the model name on an active span.

```ts
Lantern.setModel(spanId, "gpt-4");
```

### `Lantern.setInput(spanId, input)`

Sets the input on an active span.

```ts
Lantern.setInput(spanId, "Hello!");
```

### `Lantern.setTokens(spanId, inputTokens, outputTokens)`

Sets token counts on an active span.

```ts
Lantern.setTokens(spanId, 100, 50);
```

### `Lantern.setPrompt(spanId, name, version?)`

Sets the prompt name and optional version on an active span.

```ts
Lantern.setPrompt(spanId, "greeting", 1);
```

### `Lantern.getPrompt(name, options?)`

Fetches a prompt by name and label (defaults to "production"). Cached client-side for 30 seconds.

```ts
const template = await Lantern.getPrompt("greeting", "production");

// Or with options object
const template = await Lantern.getPrompt("greeting", { label: "staging" });

// Or by version (immutable cache, no TTL)
const template = await Lantern.getPrompt("greeting", { version: 2 });
```

### `Lantern.writeScore(spanId, options)`

Writes a manual score for a span. Generates a trace ID automatically.

```ts
// Object syntax
await Lantern.writeScore(spanId, {
  name: "accuracy",
  value: 0.95,
  reasoning: "Perfect answer",
});

// Shorthand: writeScore(spanId, evalName, value, reasoning?)
await Lantern.writeScore(spanId, "helpfulness", 0.8);
```

### `Lantern.flush()`

Forces export of all pending spans.

```ts
await Lantern.flush();
```

## Browser Compatibility

This SDK uses only browser-native APIs:

- `fetch` for HTTP requests
- `crypto.getRandomValues` for ID generation
- No Node.js APIs (`process`, `fs`, `net`, etc.)

Works in all modern browsers and edge runtimes (Cloudflare Workers, Deno, etc.).

## License

MIT
