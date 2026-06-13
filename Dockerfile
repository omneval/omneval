# ---- Stage 1: Build the React UI ----
FROM node:22-alpine AS ui-builder

WORKDIR /build/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ---- Stage 2: Build Go services ----
# Use Debian-based image: DuckDB's pre-compiled static library targets glibc,
# which is incompatible with Alpine's musl (missing backtrace, malloc_trim, __res_init).
FROM golang:1.25 AS go-builder

# Install C/C++ compilers and git (needed for CGO/DuckDB bindings)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy go.work and all module files
COPY go.work go.work.sum ./
COPY internal/go.* ./internal/
COPY services/ingest/go.* ./services/ingest/
COPY services/writer/go.* ./services/writer/
COPY services/query/go.* ./services/query/
COPY services/eval/go.* ./services/eval/
COPY services/quack/go.* ./services/quack/
COPY sdk/go/go.* ./sdk/go/

# Copy pricing data (needed by the embedded pricing package for build cache)
COPY internal/pricing/ ./internal/pricing/

# Copy UI dist (embedded into query service binary)
COPY --from=ui-builder /build/services/query/internal/server/ui/dist ./services/query/internal/server/ui/dist

# Download dependencies to populate the module cache before copying source,
# so that subsequent full builds only recompile changed packages.
RUN go mod download -C ./internal && \
    go mod download -C ./sdk/go && \
    go mod download -C ./services/ingest && \
    go mod download -C ./services/writer && \
    go mod download -C ./services/query && \
    go mod download -C ./services/eval && \
    go mod download -C ./services/quack

# Copy source code
COPY internal/ ./internal/
COPY services/ ./services/
COPY sdk/go/ ./sdk/go/

# Build each service binary
RUN go build -o /build/ingest ./services/ingest/cmd/ingest/
RUN go build -o /build/writer ./services/writer/cmd/writer/
RUN go build -o /build/query ./services/query/cmd/query/
RUN go build -o /build/eval ./services/eval/cmd/eval/
RUN go build -o /build/quack ./services/quack/cmd/quack/

# ---- Stage 3: Runtime image ----
# DuckDB's precompiled static library requires glibc >= 2.38; Ubuntu 24.04 ships 2.39.
FROM ubuntu:24.04

ARG TARGETARCH=amd64
# Install curl for health checks and ca-certificates for TLS, then download mc
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://dl.min.io/client/mc/release/linux-${TARGETARCH}/mc" -o /usr/local/bin/mc \
    && chmod +x /usr/local/bin/mc

COPY --from=go-builder /build/ingest /usr/local/bin/omneval-ingest
COPY --from=go-builder /build/writer /usr/local/bin/omneval-writer
COPY --from=go-builder /build/query /usr/local/bin/omneval-query
COPY --from=go-builder /build/eval /usr/local/bin/omneval-eval
COPY --from=go-builder /build/quack /usr/local/bin/omneval-quack

WORKDIR /app

# Default to omneval-ingest; docker-compose overrides this per service.
CMD ["omneval-ingest"]
