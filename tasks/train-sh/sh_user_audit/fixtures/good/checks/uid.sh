# uid.sh - UID0 and DUP_UID

check_uid() {
	local name pw uid gid gecos home shell
	while IFS=: read -r name pw uid gid gecos home shell; do
		if [ "$uid" = 0 ] && [ "$name" != root ]; then
			echo "[UID0] $name: uid 0"
		fi
	done < <(users)

	users | awk -F: '
		{ count[$3]++; names[$3] = (count[$3] > 1) ? names[$3] ", " $1 : $1 }
		END { for (u in count) if (count[u] > 1) printf "%s\t%s\n", u, names[u] }' |
		sort -t "$(printf '\t')" -k1,1n |
		while IFS=$'\t' read -r uid names; do
			echo "[DUP_UID] uid $uid: $names"
		done
}
