#!/usr/bin/env bash
# count_chunks.sh — print size and chunk count for a file.
#
# Usage: count_chunks.sh <file>
# Output (stdout, single line): size=<bytes> chunks=<nchunks>
#
# Used by the briefing skill to plan chunked processing of history files >150kb.
set -euo pipefail
FILE="${1:?usage: count_chunks.sh <file>}"
CHUNK_SIZE="${CHUNK_SIZE:-140000}"

[[ -f "$FILE" ]] || { echo "no such file: $FILE" >&2; exit 1; }

SIZE=$(wc -c < "$FILE")
NCHUNKS=$(( (SIZE + CHUNK_SIZE - 1) / CHUNK_SIZE ))
echo "size=$SIZE chunks=$NCHUNKS"
