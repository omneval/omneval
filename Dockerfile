# ---- Stage 1: Build the React UI ----
FROM node:22-alpine AS ui-builder

WORKDIR /build/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ---- Stage 2: Build Go services ----
FROM golang:1.25-alpine AS go-builder

# Install C compiler (needed for CGO/DuckDB bindings) and git
RUN apk add --no-cache gcc musl-dev git

WORKDIR /build

# Copy go.work and all module files
COPY go.work ./
COPY internal/go.* ./internal/
COPY services/ingest/go.* ./services/ingest/
COPY services/writer/go.* ./services/writer/
COPY services/query/go.* ./services/query/
COPY services/eval/go.* ./services/eval/
COPY sdk/go/go.* ./sdk/go/

# Copy pricing data (read-only mount target, but copy for build cache)
COPY internal/pricing/ ./internal/pricing/

# Copy UI dist (embedded into query service binary)
COPY --from=ui-builder /build/ui/dist ./ui/dist

# Download dependencies
RUN go build ./...

# Copy source code
COPY internal/ ./internal/
COPY services/ ./services/
COPY sdk/go/ ./sdk/go/

# Build each service binary
RUN go build -o /build/ingest ./services/ingest/cmd/ingest/
RUN go build -o /build/writer ./services/writer/cmd/writer/
RUN go build -o /build/query ./services/query/cmd/query/
RUN go build -o /build/eval ./services/eval/cmd/eval/

# ---- Stage 3: Runtime image ----
FROM alpine:3.21

# Install curl for health checks and mc for MinIO CLI
RUN apk add --no-cache curl ca-certificates minio-mc

COPY --from=go-builder /build/ingest /usr/local/bin/lantern-ingest
COPY --from=go-builder /build/writer /usr/local/bin/lantern-writer
COPY --from=go-builder /build/query /usr/local/bin/lantern-query
COPY --from=go-builder /build/eval /usr/local/bin/lantern-eval

# Pricing JSON stays in /app/internal/pricing
COPY internal/pricing /app/internal/pricing

WORKDIR /app

ENTRYPOINT ["sh", "-c"]
