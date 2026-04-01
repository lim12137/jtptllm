$df = Get-Content Dockerfile -Raw

if ($df -match 'COPY\s+dist/linux-\$\{TARGETARCH\}/proxy\s+/app/proxy') {
  throw 'Dockerfile still copies a prebuilt dist binary instead of building from source'
}

if ($df -notmatch 'FROM(?:\s+--platform=\$BUILDPLATFORM)?\s+golang:1\.22(?:\.\d+)?(?:-[^\s]+)?\s+AS\s+builder') {
  throw 'Dockerfile missing golang builder stage'
}

if ($df -notmatch 'go\s+build[\s\S]*\./cmd/proxy') {
  throw 'Dockerfile missing in-image go build for ./cmd/proxy'
}

if ($df -notmatch 'COPY\s+--from=builder\s+[^\r\n]*\s+/app/proxy') {
  throw 'Dockerfile missing copy from builder output'
}
