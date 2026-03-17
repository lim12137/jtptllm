# syntax=docker/dockerfile:1

FROM golang:1.22 AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/proxy ./cmd/proxy

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/proxy /app/proxy
EXPOSE 8022
USER nonroot:nonroot
ENTRYPOINT ["/app/proxy"]