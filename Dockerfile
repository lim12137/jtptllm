# Dockerfile
FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH
WORKDIR /app
COPY dist/linux-${TARGETARCH}/proxy /app/proxy
COPY api.txt /app/api.txt
ENV GOMEMLIMIT=512MiB
USER nonroot:nonroot
ENTRYPOINT ["/app/proxy"]
