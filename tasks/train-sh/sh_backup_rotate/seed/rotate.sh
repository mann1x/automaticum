#!/usr/bin/env bash
# rotate.sh - apply the daily/weekly/monthly retention policy to a backup tree (see README.md)
set -euo pipefail
export LC_ALL=C

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/backups.sh
. "$here/lib/backups.sh"

usage() {
	cat >&2 <<'USAGE'
usage: rotate.sh [-n|--dry-run] [--today YYYY-MM-DD] [--daily N] [--weekly N] [--monthly N] BACKUP_DIR
USAGE
	exit 2
}

dry_run=0
today=$(date +%F)
dir=""

while [ $# -gt 0 ]; do
	case $1 in
	-n | --dry-run) dry_run=1 ;;
	--today)
		today=$2
		shift
		;;
	-h | --help) usage ;;
	*) dir=$1 ;;
	esac
	shift
done

[ -d "$dir" ] || {
	echo "rotate: no such directory: $dir" >&2
	exit 2
}

# TODO: retention counts (--daily/--weekly/--monthly), the plan, the report and the deletions.
echo "rotate: not implemented yet (dry_run=$dry_run today=$today)" >&2
exit 3
