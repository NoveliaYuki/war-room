#!/usr/bin/env bash

# ====================================================================
# War Room - Selection Process Command Center Launcher
# ====================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PORT="${PORT:-4040}"
export PORT

echo "======================================================"
echo "🛡️  Starting War Room Command Center"
echo "📍 Port: $PORT"
echo "📁 Root: $SCRIPT_DIR"
echo "======================================================"

if ! command -v go &> /dev/null; then
  echo "❌ Go was not found in PATH."
  echo "Please install Go or run the application with Docker Compose."
  exit 1
fi

echo "🚀 Running the Go backend..."
exec go run ./backend/cmd/server
