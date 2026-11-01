#!/bin/bash
export PATH="/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
cd -- "$(dirname -- "$0")"
exec /bin/bash ./Start.sh "$@"
