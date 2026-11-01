#!/bin/bash
export PATH="/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
set -euo pipefail
cd -- "$(dirname -- "$0")"
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) server="bin/game-server" ;;
  Darwin-arm64) server="bin/game-server-macos-arm64" ;;
  *) server="" ;;
esac
export OLLAMA_ENABLED="${OLLAMA_ENABLED:-true}"
export DEV_WEB_DIR="$PWD/web"
if [[ ! -f "$DEV_WEB_DIR/css/flight-deck.css" ]]; then echo "Frontend missing. Download the complete project." >&2; exit 1; fi
printf '%s\n' 'Open http://localhost:8080 in your browser.' 'Other players: use Invite in the game and join the same room.' 'Stop the host with Ctrl+C.'
if [[ -n "$server" && -x "$server" ]]; then
  exec "./$server" "$@"
fi
if command -v go >/dev/null 2>&1; then
  exec go run . "$@"
fi
echo "Install Go 1.23+ to run from source, or build the release binaries first. See README.md." >&2
exit 1
