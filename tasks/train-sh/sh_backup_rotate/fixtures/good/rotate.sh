#!/usr/bin/env bash
# rotate.sh - apply the daily/weekly/monthly retention policy to a backup tree (see README.md)
set -euo pipefail
export LC_ALL=C

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/backups.sh
. "$here/lib/backups.sh"

usage() {
	cat >&2 <<'EOF'
usage: rotate.sh [-n|--dry-run] [--today YYYY-MM-DD] [--daily N] [--weekly N] [--monthly N] BACKUP_DIR
EOF
	exit 2
}

die() {
	echo "rotate: $*" >&2
	exit 2
}

dry_run=0
today=$(date +%F)
keep_daily=7
keep_weekly=4
keep_monthly=6
dir=""

need_count() {
	case $2 in '' | *[!0-9]*) die "$1 needs a whole number, got '$2'" ;; esac
}

while [ $# -gt 0 ]; do
	case $1 in
	-n | --dry-run) dry_run=1 ;;
	--today | --daily | --weekly | --monthly)
		[ $# -ge 2 ] || die "$1 needs a value"
		case $1 in
		--today) today=$2 ;;
		--daily) need_count "$1" "$2" && keep_daily=$2 ;;
		--weekly) need_count "$1" "$2" && keep_weekly=$2 ;;
		--monthly) need_count "$1" "$2" && keep_monthly=$2 ;;
		esac
		shift
		;;
	-h | --help) usage ;;
	--)
		shift
		break
		;;
	-*) die "unknown option $1" ;;
	*)
		[ -z "$dir" ] || usage
		dir=$1
		;;
	esac
	shift
done
if [ $# -gt 0 ]; then
	if [ -n "$dir" ] || [ $# -ne 1 ]; then
		usage
	fi
	dir=$1
fi
[ -n "$dir" ] || usage
[ -d "$dir" ] || die "no such directory: $dir"
is_valid_date "$today" || die "--today must be a date YYYY-MM-DD, got '$today'"
if [ $((10#$keep_daily + 10#$keep_weekly + 10#$keep_monthly)) -eq 0 ]; then
	die "refusing to run with every retention count at 0 (that would delete every backup)"
fi
keep_daily=$((10#$keep_daily))
keep_weekly=$((10#$keep_weekly))
keep_monthly=$((10#$keep_monthly))
dir=${dir%/}
[ -n "$dir" ] || dir=/
case $dir in -*) dir=./$dir ;; esac

tab=$'\t'
records=""
while IFS= read -r -d '' path; do
	rel=${path#"$dir"/}
	base=${rel##*/}
	sub=${rel%"$base"}
	case $base in *.sha256) continue ;; esac
	parse_backup_name "$base" || continue
	if ! is_valid_date "$BK_DATE"; then
		echo "rotate: skipping $rel: invalid date $BK_DATE" >&2
		continue
	fi
	records+="$sub$BK_SET/$BK_EXT$tab$BK_DATE$BK_TIME$tab$BK_DATE$tab$rel"$'\n'
done < <(find "$dir" -mindepth 1 -type f -print0)

declare -A plan=()
set_key=""
while IFS=$tab read -r key _stamp day rel; do
	[ -n "$key" ] || continue
	if [ "$key" != "$set_key" ]; then
		set_key=$key
		n_daily=0 n_weekly=0 n_monthly=0
		last_day="" last_week="" last_month=""
	fi
	if [[ $day > $today ]]; then
		plan[$rel]="keep (future)"
		continue
	fi
	reasons=""
	if [ "$day" != "$last_day" ] && [ "$n_daily" -lt "$keep_daily" ]; then
		reasons+=",daily"
		n_daily=$((n_daily + 1))
		last_day=$day
	fi
	week=$(week_of "$day")
	if [ "$week" != "$last_week" ] && [ "$n_weekly" -lt "$keep_weekly" ]; then
		reasons+=",weekly"
		n_weekly=$((n_weekly + 1))
		last_week=$week
	fi
	month=$(month_of "$day")
	if [ "$month" != "$last_month" ] && [ "$n_monthly" -lt "$keep_monthly" ]; then
		reasons+=",monthly"
		n_monthly=$((n_monthly + 1))
		last_month=$month
	fi
	if [ -n "$reasons" ]; then
		plan[$rel]="keep (${reasons#,})"
	else
		plan[$rel]="delete"
	fi
done < <(printf '%s' "$records" | sort -t "$tab" -k1,1 -k2,2r)

kept=0
deleted=0
failed=0
while IFS= read -r rel; do
	[ -n "$rel" ] || continue
	action=${plan[$rel]}
	if [ "$action" = "delete" ]; then
		echo "delete $rel"
		if [ "$dry_run" -eq 1 ]; then
			deleted=$((deleted + 1))
		elif rm -f -- "$dir/$rel" 2>/dev/null && [ ! -e "$dir/$rel" ]; then
			rm -f -- "$dir/$rel.sha256" 2>/dev/null || true
			deleted=$((deleted + 1))
		else
			echo "rotate: cannot delete $rel" >&2
			failed=$((failed + 1))
		fi
	else
		echo "${action%% *} $rel ${action#* }"
		kept=$((kept + 1))
	fi
done < <(for rel in "${!plan[@]}"; do printf '%s\n' "$rel"; done | sort)

if [ "$dry_run" -eq 1 ]; then
	echo "kept $kept, would delete $deleted"
elif [ "$failed" -gt 0 ]; then
	echo "kept $kept, deleted $deleted, failed $failed"
	exit 1
else
	echo "kept $kept, deleted $deleted"
fi
exit 0
