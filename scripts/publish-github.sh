#!/bin/bash
# Initial publication only. Run in a terminal with GitHub access and Git write permission.
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
target_url='https://github.com/notbrokenprogrammer/hackathon-2026-agentdeck-mcp-rag.git'
public_name='notbrokenprogrammer'
public_email='notbrokenprogrammer@users.noreply.github.com'

if git rev-parse --verify HEAD >/dev/null 2>&1; then
  echo 'This helper only publishes a project without existing commits. Review existing history before pushing.' >&2
  exit 1
fi
remote_refs="$(git ls-remote "$target_url")"
if [[ -n "$remote_refs" ]]; then
  echo 'The destination already contains Git refs. Stop here and integrate its history before publishing.' >&2
  exit 1
fi

git init -b main
existing_origin="$(git remote get-url origin 2>/dev/null || true)"
if [[ -n "$existing_origin" && "$existing_origin" != "$target_url" ]]; then
  echo 'Origin points to a different repository; no changes were pushed.' >&2
  exit 1
fi
if [[ -z "$existing_origin" ]]; then
  git remote add origin "$target_url"
fi
git add --all
python3 scripts/check-publication.py --staged
git -c user.name="$public_name" -c user.email="$public_email" commit -m 'Prepare AgentDeck hackathon project: agents, MCP and hybrid RAG'

# Environment identity overrides take priority over Git config: check the actual commit.
if [[ "$(git show -s --format='%an <%ae>|%cn <%ce>' HEAD)" != "$public_name <$public_email>|$public_name <$public_email>" ]]; then
  echo 'Commit identity differs from the intended public identity. No push performed.' >&2
  exit 1
fi
git push --set-upstream origin HEAD:main
local_head="$(git rev-parse HEAD)"
remote_head="$(git ls-remote "$target_url" refs/heads/main | cut -f1)"
[[ "$local_head" == "$remote_head" ]] || { echo 'Remote verification failed.' >&2; exit 1; }
printf 'Published and verified: %s\nCommit: %s\n' "$target_url" "$local_head"
