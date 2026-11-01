#!/bin/bash
export PATH="/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
set -euo pipefail
cd -- "$(dirname -- "$0")"
export OLLAMA_ENABLED=true
export OLLAMA_URL="${OLLAMA_URL:-http://127.0.0.1:11434}"
export OLLAMA_MODEL="${OLLAMA_MODEL:-qwen3:4b-instruct-2507-q4_K_M}"
printf '%s\n' 'Ollama must already be running with the selected local model downloaded.'
exec /bin/bash ./Start.sh "$@"
