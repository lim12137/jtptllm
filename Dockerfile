# Dockerfile
FROM --platform=$BUILDPLATFORM golang:1.22 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

# Copy dependency files first for better layer caching
COPY go.mod ./
COPY go.sum ./ 2>/dev/null || true

# Download dependencies (cached layer)
RUN go mod download || true

# Copy source code
COPY cmd ./cmd
COPY internal ./internal

# Build the binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/proxy ./cmd/proxy

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /out/proxy /app/proxy
COPY --chown=nonroot:nonroot api.txt /app/api.txt 2>/dev/null || true

ENV GOMEMLIMIT=512MiB

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/proxy", "-health"] || exit 1

ENTRYPOINT ["/app/proxy"]
