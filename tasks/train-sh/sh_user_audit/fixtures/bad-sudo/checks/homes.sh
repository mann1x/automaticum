# homes.sh - WW_HOME and HOME_OWNER (from homes.manifest)

check_homes() {
	local manifest="$DATA/homes.manifest"
	[ -r "$manifest" ] || return 0
	local mode owner path name pw uid gid gecos home shell key
	local -A hmode=() howner=()
	while read -r mode owner path; do
		case $mode in '' | '#'*) continue ;; esac
		[[ $mode =~ ^[0-7]{3,4}$ ]] || continue
		[ -n "$path" ] || continue
		key=$(norm_path "$path")
		hmode[$key]=$mode
		howner[$key]=$owner
	done <"$manifest"

	local ww="" own=""
	while IFS=: read -r name pw uid gid gecos home shell; do
		[ -n "$home" ] || continue
		key=$(norm_path "$home")
		[ -n "${hmode[$key]+set}" ] || continue
		mode=${hmode[$key]}
		case ${mode: -1} in 2 | 3 | 6 | 7)
			ww+="[WW_HOME] $name: $home is world-writable ($mode)"$'\n'
			;;
		esac
		if is_login_shell "$shell" && [ "${howner[$key]}" != "$name" ]; then
			own+="[HOME_OWNER] $name: $home is owned by ${howner[$key]}"$'\n'
		fi
	done < <(users)
	printf '%s' "$ww$own"
}
