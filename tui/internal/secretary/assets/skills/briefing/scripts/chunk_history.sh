#!/usr/bin/env bash
# chunk_history.sh — print one chunk of a history file by 1-indexed chunk number.
#
# Usage: chunk_history.sh <file> <chunk_n>
# Prints chunk N (size = CHUNK_SIZE bytes, default 140000) to stdout.
# Also prints chunk metadata to stderr: "chunk N of NCHUNKS, bytes X-Y of TOTAL".
#
# Used by the briefing skill to split history files >150kb into processable pieces.
set -euo pipefail
FILE="${1:?usage: chunk_history.sh <file> <chunk_n>}"
N="${2:?usage: chunk_history.sh <file> <chunk_n>}"
CHUNK_SIZE="${CHUNK_SIZE:-140000}"

[[ -f "$FILE" ]] || { echo "no such file: $FILE" >&2; exit 1; }

SIZE=$(wc -c < "$FILE")
NCHUNKS=$(( (SIZE + CHUNK_SIZE - 1) / CHUNK_SIZE ))

(( N >= 1 && N <= NCHUNKS )) || {
  echo "chunk $N out of range (have $NCHUNKS chunks for $SIZE bytes)" >&2
  exit 1
}

START=$(( CHUNK_SIZE * (N - 1) ))
END=$(( CHUNK_SIZE * N ))
(( END > SIZE )) && END=$SIZE

echo "chunk $N of $NCHUNKS, bytes $((START+1))-$END of $SIZE" >&2
head -c "$END" "$FILE" | tail -c +$((START + 1))
