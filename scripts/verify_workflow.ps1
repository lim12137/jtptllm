$wf = Get-Content .github/workflows/docker-build.yml -Raw
$checks = @(
  'actions/setup-go',
  'go test ./... -v',
  'dist/linux-amd64/proxy',
  'dist/linux-arm64/proxy',
  'dist/windows-amd64/proxy.exe',
  'docker/build-push-action@v6',
  'ghcr.io/${{ github.repository }}:latest'
)
foreach ($c in $checks) {
  if ($wf -notmatch [regex]::Escape($c)) { throw "Missing $c" }
}
