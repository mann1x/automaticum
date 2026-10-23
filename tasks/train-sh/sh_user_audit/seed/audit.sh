#!/usr/bin/env bash
# audit.sh - offline account audit over a snapshot of /etc (see README.md)
set -euo pipefail
export LC_ALL=C

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

usage() {
	echo "usage: audit.sh DATA_DIR" >&2
	exit 2
}

[ $# -eq 1 ] || usage
DATA=${1%/}
[ -n "$DATA" ] || DATA=/
if [ ! -d "$DATA" ] || [ ! -r "$DATA/passwd" ]; then
	echo "audit: $1 is not a snapshot directory with a readable passwd file" >&2
	exit 2
fi

# shellcheck source=lib/common.sh
. "$here/lib/common.sh"
for check in uid aging homes keys sudo; do
	# shellcheck source=/dev/null
	. "$here/checks/$check.sh"
done

# Findings are grouped by code in this order; within a code they keep the order the check printed.
codes="UID0 DUP_UID NO_AGING WW_HOME HOME_OWNER KEY_NO_COMMENT KEY_BAD SUDO_NOPASSWD"

findings=$(
	{
		check_uid
		check_aging
		check_homes
		check_keys
		check_sudo
	} | awk -v codes="$codes" '
		BEGIN { n = split(codes, c, " "); for (i = 1; i <= n; i++) rank[c[i]] = i }
		{
			code = substr($1, 2, length($1) - 2)
			printf "%d\t%d\t%s\n", (code in rank) ? rank[code] : 99, NR, $0
		}' | sort -t "$(printf '\t')" -k1,1n -k2,2n | cut -f3-
)

if [ -z "$findings" ]; then
	echo "findings: 0"
	exit 0
fi
printf '%s\n' "$findings"
echo "findings: $(printf '%s\n' "$findings" | wc -l)"
exit 1
