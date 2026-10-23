#!/usr/bin/env bash
# logsum.sh - summarise web-server access logs (see README.md for the report format)
set -euo pipefail
export LC_ALL=C

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
threshold=500
top=10

usage() {
	echo "usage: logsum.sh [-t SLOW_MS] [-n TOP] LOGFILE..." >&2
	exit 2
}

while getopts ":t:n:" opt; do
	case $opt in
	t) threshold=$OPTARG ;;
	n) top=$OPTARG ;;
	*) usage ;;
	esac
done
shift $((OPTIND - 1))
[ $# -ge 1 ] || usage
case $threshold in '' | *[!0-9]*)
	echo "logsum: -t needs a whole number of milliseconds" >&2
	exit 2
	;;
esac
case $top in '' | *[!0-9]*)
	echo "logsum: -n needs a whole number" >&2
	exit 2
	;;
esac

for f in "$@"; do
	if [ ! -r "$f" ]; then
		echo "logsum: cannot read $f" >&2
		exit 1
	fi
done

read_logs() {
	cat $@
}

records=$(mktemp "${TMPDIR:-/tmp}/logsum.XXXXXX")
trap 'rm -f "$records"' EXIT

read_logs "$@" | awk -f "$here/../lib/parse.awk" >"$records"

IFS=$'\t' read -r _ lines parsed malformed < <(grep '^C' "$records")

echo "== summary =="
echo "lines: $lines"
echo "parsed: $parsed"
echo "malformed: $malformed"

echo "== top paths =="
awk '/^R\t/' "$records" | cut -f5 | sort | uniq -c |
	awk '{ print $1 " " $2 }' | sort -rn | awk -v n="$top" 'NR <= n'

echo "== status by hour =="
awk '/^R\t/' "$records" | cut -f3,6 | sort | uniq -c |
	awk '{
		if ($2 != cur) {
			if (cur != "") print line
			cur = $2
			line = $2
		}
		line = line " " $3 "=" $1
	}
	END { if (cur != "") print line }'

echo "== slow requests (>= $threshold ms) =="
awk -F'\t' -v t="$threshold" '$1 == "R" && $7 > t + 0 { print $2 " " $7 " " $4 " " $5 }' "$records"

exit 0
