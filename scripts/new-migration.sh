#!/usr/bin/env bash
# Creates the next goose migration: internal/db/migrations/NNNNNNNNNNNN_<name>.sql
#
# usage: scripts/new-migration.sh <snake_case_name>
#
# The number is one more than the highest existing 12-digit prefix, so it
# always sorts after every migration already in the tree; the first one is
# 000000000001. MIGRATIONS_DIR overrides the directory (tests use it).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="${MIGRATIONS_DIR:-$ROOT/internal/db/migrations}"
NAME="${1:-}"

if [[ ! "$NAME" =~ ^[a-z][a-z0-9_]*$ ]]; then
	echo "usage: make new-migration NAME=snake_case (lowercase letters, digits and underscores, starting with a letter)" >&2
	exit 1
fi

mkdir -p "$DIR"
max=0
shopt -s nullglob
for f in "$DIR"/[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]_*.sql; do
	n="$(basename "$f")"
	n=$((10#${n:0:12}))
	((n > max)) && max=$n
done

file="$(printf '%s/%012d_%s.sql' "$DIR" $((max + 1)) "$NAME")"
cat >"$file" <<SQL
-- +goose Up
-- Write the schema change here. Wrap PL/pgSQL bodies in StatementBegin and
-- StatementEnd, and guard with IF NOT EXISTS.

-- +goose Down
-- Write the exact reverse of Up here, guarded with IF EXISTS. If it cannot be
-- reversed, keep this section and say why.
SQL
echo "${file#"$ROOT"/}"
