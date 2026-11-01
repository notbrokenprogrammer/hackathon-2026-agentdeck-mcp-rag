#!/bin/sh
set -eu
LAST_CREW_PREVIEW_PORT=${PORT:-4173}
while [ "$#" -gt 0 ]; do
 case "$1" in
  --port) LAST_CREW_PREVIEW_PORT="$2"; shift 2;;
  *) shift;;
 esac
done
export HOST_ADDR="0.0.0.0:$LAST_CREW_PREVIEW_PORT"
export DATA_DIR="${LASTCREW_PREVIEW_DATA_DIR:-.preview-data}"
export DEV_WEB_DIR=web
cd -- "$(dirname -- "$0")/.."
exec go run .
