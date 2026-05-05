#!/bin/bash
set -euo pipefail

# Build the React UI
cd "$(dirname "$0")/../.."
npm --prefix ui run build

# Copy the built UI to the server package for embed.FS
rm -rf services/query/internal/server/ui/dist
cp -r services/query/ui/dist services/query/internal/server/ui/dist

# Build the Go binary
go build -o services/query/cmd/query/query ./services/query/cmd/query/

echo "✓ Query API binary built successfully"
