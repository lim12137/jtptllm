$df = Get-Content Dockerfile -Raw
if ($df -notmatch 'dist/linux-\${TARGETARCH}/proxy') { throw 'Dockerfile missing dist path' }
