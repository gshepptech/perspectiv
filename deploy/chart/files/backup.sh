#!/bin/sh
# Writes one pg_dump custom-format archive of the perspectiv database into
# BACKUP_DIR and keeps only the newest BACKUP_KEEP archives there.
#
# The chart runs this from a CronJob every day at 03:15 UTC with
# BACKUP_KEEP=14. The archive is written under a hidden partial name, checked
# with pg_restore --list, and only then renamed into place, so a failed or
# interrupted run never leaves a file that looks like a good backup and never
# triggers pruning. Connection settings come from the standard libpq variables
# (PGHOST, PGUSER, PGDATABASE, PGPASSWORD).
#
# Restore an archive with:
#   pg_restore --clean --if-exists --no-owner -d perspectiv <archive>
set -eu

: "${BACKUP_DIR:?BACKUP_DIR is required}"
: "${BACKUP_KEEP:?BACKUP_KEEP is required}"
case "$BACKUP_KEEP" in
'' | *[!0-9]* | 0)
	echo "backup: BACKUP_KEEP must be a positive integer, got '$BACKUP_KEEP'" >&2
	exit 2
	;;
esac

stamp=$(date -u +%Y%m%dT%H%M%SZ)
final="$BACKUP_DIR/perspectiv-$stamp.dump"
partial="$BACKUP_DIR/.perspectiv-$stamp.dump.partial"
if [ -e "$final" ]; then
	echo "backup: $final already exists" >&2
	exit 1
fi
trap 'rm -f "$partial"' EXIT

# A new pod's first connection can be refused while the NetworkPolicy
# controller admits its address; wait for the server before dumping.
tries=0
until pg_isready -q; do
	tries=$((tries + 1))
	if [ "$tries" -ge 30 ]; then
		echo "backup: $PGHOST did not accept connections within 60s" >&2
		exit 1
	fi
	sleep 2
done

pg_dump --format=custom --file="$partial"
pg_restore --list "$partial" >/dev/null
mv "$partial" "$final"
echo "backup: wrote $final ($(wc -c <"$final") bytes)"

# Archive names sort by timestamp, so the newest come first in reverse order.
for f in "$BACKUP_DIR"/perspectiv-*.dump; do
	[ -e "$f" ] && printf '%s\n' "${f##*/}"
done | grep -E '^perspectiv-[0-9]{8}T[0-9]{6}Z\.dump$' | sort -r |
	tail -n +"$((BACKUP_KEEP + 1))" | while read -r old; do
	rm -f "$BACKUP_DIR/$old"
	echo "backup: pruned $old"
done
