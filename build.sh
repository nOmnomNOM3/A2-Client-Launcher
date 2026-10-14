#!/bin/sh
# Build the Windows launcher. Usage: VERSION=1.0.0 PUBKEY=<base64> ./build.sh
set -e
VERSION=${VERSION:-0.0.0-dev}
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest cmd/a2launcher/launcher.manifest -o cmd/a2launcher/rsrc_windows_amd64.syso
mkdir -p dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -H windowsgui -X main.version=$VERSION -X main.publicKey=$PUBKEY ${MANIFEST_URL:+-X main.manifestURL=$MANIFEST_URL}" \
  -o "dist/RoM2 Launcher.exe" ./cmd/a2launcher
ls -l dist
