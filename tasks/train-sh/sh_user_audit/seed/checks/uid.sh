# uid.sh - UID0 and DUP_UID

check_uid() {
	local name pw uid gid gecos home shell names
	while IFS=: read -r name pw uid gid gecos home shell; do
		if [ "$uid" = 0 ] && [ "$name" != root ]; then
			echo "[UID0] $name: uid 0"
		fi
	done < <(users)

	for uid in $(users | cut -d: -f3 | sort -n | uniq -d); do
		names=$(grep ":$uid:" "$DATA/passwd" | cut -d: -f1 | paste -sd, - | sed 's/,/, /g')
		echo "[DUP_UID] uid $uid: $names"
	done
}
