# Binary-First Image Build Design (2026-03-18)

## Goal
Build container images directly from precompiled Go binaries to reduce build time, image size, memory usage, and improve runtime performance. Produce Linux multi-arch images and Windows amd64 build artifacts.

## Scope
- GitHub Actions builds Go binaries first, then uses Docker Buildx to package them.
- Image tags follow `ghcr.io/<org>/<repo>:latest` via `${{ github.repository }}`.
- Linux images: `linux/amd64` + `linux/arm64`.
- Windows amd64 binary is produced as a build artifact (not included in the image).

## Non-goals
- No changes to application runtime behavior or business logic.
- No release tagging strategy changes beyond `latest`.

## Architecture
1. CI compiles binaries into `dist/` for each target OS/arch.
2. Docker Buildx uses a minimal runtime image and selects the correct binary per `TARGETARCH`.
3. Multi-arch image is pushed to GHCR.

## Workflow Changes
- Add a Go build step in the workflow to produce:
  - `dist/linux-amd64/proxy`
  - `dist/linux-arm64/proxy`
  - `dist/windows-amd64/proxy.exe`
- Add `go test ./... -v` before build.
- Set image tag to `ghcr.io/${{ github.repository }}:latest`.

## Dockerfile Design
- Runtime-only Dockerfile at repo root:
  - Base: `gcr.io/distroless/static` (preferred for minimal footprint with basic tooling).
  - `ARG TARGETARCH` used to copy `dist/linux-${TARGETARCH}/proxy` into `/app/proxy`.
  - `ENTRYPOINT ["/app/proxy"]`.

## Performance & Memory Measures
- Build flags: `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, `-ldflags "-s -w"` to reduce binary size.
- Minimal runtime image to reduce memory overhead.
- Default `GOMEMLIMIT=512MiB` baked into Dockerfile to stabilize memory usage; can be overridden at deploy time.

## Risks & Mitigations
- Missing binary for target arch -> Docker build fails. Mitigation: CI build step produces all target binaries.
- GHCR push failures -> workflow fail fast; permissions already scoped.

## Testing
- `go test ./... -v` executed in workflow before build.

## Success Criteria
- Workflow builds and pushes multi-arch image successfully.
- `latest` image runs with expected performance and reduced memory footprint.
- Windows amd64 binary available as CI artifact.
