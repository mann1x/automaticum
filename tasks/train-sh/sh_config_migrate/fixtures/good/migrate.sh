#!/usr/bin/env bash
# migrate.sh - convert the old INI service configs to the flat key=value layout (see README.md)
set -euo pipefail
export LC_ALL=C

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

usage() {
	echo "usage: migrate.sh MAPPING SRC_DIR DEST_DIR" >&2
	exit 2
}

[ $# -eq 3 ] || usage
mapping=$1
src=$2
dest=$3
if [ ! -r "$mapping" ]; then
	echo "migrate: cannot read mapping file $mapping" >&2
	exit 2
fi
if [ ! -d "$src" ]; then
	echo "migrate: no such directory $src" >&2
	exit 2
fi
mkdir -p -- "$dest"

total=0
migrated=0
errors=0
warnings=0

while IFS= read -r -d '' path; do
	name=${path##*/}
	conf="$dest/${name%.ini}.conf"
	total=$((total + 1))

	out=$(awk -v mapfile="$mapping" -f "$here/lib/ini2flat.awk" "$path")

	findings=$(printf '%s\n' "$out" | awk -F'\t' '$1 == "F"' | sort -t "$(printf '\t')" -k2,2n -k3,3n)
	ferr=$(printf '%s\n' "$findings" | awk -F'\t' '$4 == "error"' | wc -l)
	fwarn=$(printf '%s\n' "$findings" | awk -F'\t' '$4 == "warning"' | wc -l)
	errors=$((errors + ferr))
	warnings=$((warnings + fwarn))
	if [ -n "$findings" ]; then
		printf '%s\n' "$findings" | awk -F'\t' -v f="$name" '{ print f ":" $2 ": " $4 ": " $5 }'
	fi

	if [ "$ferr" -eq 0 ]; then
		{
			echo "# migrated from $name"
			printf '%s\n' "$out" | awk -F'\t' '$1 == "K" { print $2 }' | sort -t= -k1,1
		} >"$conf"
		migrated=$((migrated + 1))
	else
		rm -f -- "$conf"
	fi
done < <(find "$src" -maxdepth 1 -type f -name '*.ini' -print0 | sort -z)

echo "migrated $migrated of $total files, $errors errors, $warnings warnings"
[ "$errors" -eq 0 ] || exit 1
exit 0
