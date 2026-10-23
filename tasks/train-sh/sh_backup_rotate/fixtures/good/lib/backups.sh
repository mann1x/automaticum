# backups.sh - helpers for rotate.sh (sourced, not run)

# parse_backup_name NAME
# Splits a backup file name `<set>-<YYYY-MM-DD>[_<HHMM>].<ext>` into
#   BK_SET  BK_DATE  BK_TIME (HHMM, "0000" when absent)  BK_EXT
# Returns 1 when NAME is not a backup name.
parse_backup_name() {
	local re='^(.+)-([0-9]{4}-[0-9]{2}-[0-9]{2})(_([0-9]{4}))?\.([A-Za-z0-9][A-Za-z0-9.]*)$'
	[[ $1 =~ $re ]] || return 1
	BK_SET=${BASH_REMATCH[1]}
	BK_DATE=${BASH_REMATCH[2]}
	BK_TIME=${BASH_REMATCH[4]:-0000}
	BK_EXT=${BASH_REMATCH[5]}
}

# is_valid_date YYYY-MM-DD -- true for a real calendar date
is_valid_date() {
	[[ $1 =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || return 1
	[ "$(date -d "$1" +%F 2>/dev/null)" = "$1" ]
}

# week_of YYYY-MM-DD -- the ISO 8601 week the date belongs to, e.g. 2026-W01
week_of() {
	date -d "$1" +%G-W%V
}

# month_of YYYY-MM-DD -- e.g. 2026-01
month_of() {
	printf '%s\n' "${1:0:7}"
}
