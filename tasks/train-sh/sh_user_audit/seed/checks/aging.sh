# aging.sh - NO_AGING

check_aging() {
	local name pw uid gid gecos home shell entry max
	while IFS=: read -r name pw uid gid gecos home shell; do
		is_login_shell "$shell" || continue
		entry=""
		if [ -r "$DATA/shadow" ]; then
			entry=$(awk -F: -v u="$name" '$1 == u { print; exit }' "$DATA/shadow")
		fi
		if [ -z "$entry" ]; then
			echo "[NO_AGING] $name: login shell $shell, no shadow entry"
			continue
		fi
		max=$(cut -d: -f4 <<<"$entry")
		if [ -z "$max" ] || [ "$max" = 99999 ]; then
			echo "[NO_AGING] $name: login shell $shell, no maximum password age"
		fi
	done < <(users)
}
