package lantern

import "context"

// Configure initialises the global OTLP HTTP exporter pointing at the Lantern
// ingest endpoint. Must be called once at application startup before any spans
// are started.
func Configure(endpoint, apiKey string) error {
	panic("not implemented")
}

// StartSpan starts a new span as a child of the span in ctx (if any) and
// returns a new context carrying the active span.
func StartSpan(ctx context.Context, name string) context.Context {
	panic("not implemented")
}

// EndSpan ends the span carried in ctx. Must be called with the context
// returned by StartSpan — typically deferred immediately after the call.
func EndSpan(ctx context.Context) {
	panic("not implemented")
}

// SetInput attaches the prompt input (JSON array of {role,content} objects
// or plain string) to the active span in ctx.
func SetInput(ctx context.Context, input string) {
	panic("not implemented")
}

// SetOutput attaches the completion output to the active span in ctx.
func SetOutput(ctx context.Context, output string) {
	panic("not implemented")
}

// SetModel attaches the model name to the active span in ctx.
func SetModel(ctx context.Context, model string) {
	panic("not implemented")
}

// SetTokens attaches input and output token counts to the active span in ctx.
func SetTokens(ctx context.Context, inputTokens, outputTokens int64) {
	panic("not implemented")
}
