# Dockerfile
FROM --platform=$BUILDPLATFORM golang:1.22 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/proxy ./cmd/proxy

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /out/proxy /app/proxy
COPY api.txt /app/api.txt
ENV GOMEMLIMIT=512MiB
USER nonroot:nonroot
ENTRYPOINT ["/app/proxy"]
