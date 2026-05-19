package omneval

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Global tracer provider managed by the SDK.
var (
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
	mu             sync.Mutex
)

// Configure initialises the global OTLP HTTP exporter pointing at the Omneval
// ingest endpoint. Must be called once at application startup before any spans
// are started.
func Configure(endpoint, apiKey string) error {
	host, scheme, err := sanitizeEndpoint(endpoint)
	if err != nil {
		return err
	}

	// Stop existing provider if any.
	mu.Lock()
	if tracerProvider != nil {
		_ = tracerProvider.Shutdown(context.Background())
		tracerProvider = nil
	}
	mu.Unlock()

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
		otlptracehttp.WithHeaders(map[string]string{
			"X-API-Key": apiKey,
		}),
	}
	// Use HTTP instead of HTTPS when the endpoint uses http://.
	if scheme == "http" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName("omneval-sdk"),
		),
	)
	if err != nil {
		exporter.Shutdown(context.Background())
		return fmt.Errorf("resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)

	mu.Lock()
	tracerProvider = provider
	tracer = provider.Tracer("github.com/omneval/omneval/sdk/go")
	mu.Unlock()

	return nil
}

// StartSpan starts a new span as a child of the span in ctx (if any) and
// returns a new context carrying the active span.
func StartSpan(ctx context.Context, name string) context.Context {
	mu.Lock()
	t := tracer
	mu.Unlock()

	if t == nil {
		// Configure hasn't been called — return ctx unchanged.
		return ctx
	}

	_, span := t.Start(ctx, name)
	return ctxWithSpan(ctx, span)
}

// EndSpan ends the span carried in ctx. Must be called with the context
// returned by StartSpan — typically deferred immediately after the call.
func EndSpan(ctx context.Context) {
	span := spanFromCtx(ctx)
	if span != nil {
		span.End()
	}
}

// SetInput attaches the prompt input (JSON array of {role,content} objects
// or plain string) to the active span in ctx.
func SetInput(ctx context.Context, input string) {
	span := spanFromCtx(ctx)
	if span != nil {
		span.SetAttributes(attribute.String("omneval.input", input))
	}
}

// SetOutput attaches the completion output to the active span in ctx.
func SetOutput(ctx context.Context, output string) {
	span := spanFromCtx(ctx)
	if span != nil {
		span.SetAttributes(attribute.String("omneval.output", output))
	}
}

// SetModel attaches the model name to the active span in ctx.
func SetModel(ctx context.Context, model string) {
	span := spanFromCtx(ctx)
	if span != nil {
		span.SetAttributes(attribute.String("gen_ai.request.model", model))
	}
}

// SetTokens attaches input and output token counts to the active span in ctx.
func SetTokens(ctx context.Context, inputTokens, outputTokens int64) {
	span := spanFromCtx(ctx)
	if span != nil {
		span.SetAttributes(
			attribute.Int64("gen_ai.usage.input_tokens", inputTokens),
			attribute.Int64("gen_ai.usage.output_tokens", outputTokens),
		)
	}
}

// Flush blocks until all pending spans have been exported.
// This is primarily useful for tests; in production, use Shutdown at exit.
func Flush() {
	mu.Lock()
	provider := tracerProvider
	mu.Unlock()

	if provider == nil {
		return
	}

	// Force a synchronous export. The batch processor will flush on shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = provider.ForceFlush(ctx)
}

// Shutdown shuts down the global tracer provider, flushing any pending spans.
func Shutdown() error {
	mu.Lock()
	provider := tracerProvider
	mu.Unlock()

	if provider == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return provider.Shutdown(ctx)
}

// sanitizeEndpoint validates the endpoint URL and extracts just the host:port
// component. The OTLP HTTP exporter auto-appends /v1/traces, so we strip the
// path. We also return the scheme so the caller can configure HTTP/HTTPS.
func sanitizeEndpoint(endpoint string) (string, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("endpoint scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("endpoint must include host, got %q", endpoint)
	}
	// Strip path — OTLP exporter auto-appends /v1/traces.
	u.Path = ""
	u.RawQuery = ""
	return u.Host, u.Scheme, nil
}

// ---- Context propagation helpers ----

// spanCtxKey is the context key for carrying the active span.
type spanCtxKey struct{}

func ctxWithSpan(ctx context.Context, span trace.Span) context.Context {
	return context.WithValue(ctx, spanCtxKey{}, span)
}

func spanFromCtx(ctx context.Context) trace.Span {
	v := ctx.Value(spanCtxKey{})
	if v == nil {
		return nil
	}
	span, _ := v.(trace.Span)
	return span
}
