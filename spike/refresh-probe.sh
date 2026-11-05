#!/bin/sh
# Rewrites spike/refresh-probe.md every 3s via atomic rename (how the real tool will write).
# Open it in Zed with the preview to the side (cmd-k v); the counter should tick.
set -eu
out="$(dirname "$0")/refresh-probe.md"
i=0
while :; do
  i=$((i+1))
  tmp="$out.tmp.$$"
  printf '# Refresh probe\n\ntick %s at %s\n\n```mermaid\nflowchart LR\n  a["tick %s"] --> b["%s"]\n```\n' \
    "$i" "$(date +%T)" "$i" "$(date +%T)" > "$tmp"
  mv "$tmp" "$out"
  sleep 3
done
