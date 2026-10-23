# common.sh - helpers shared by the checks (sourced by audit.sh; DATA is the snapshot directory)

# users: the valid passwd entries (7 colon-separated fields, not a comment), in file order.
# Read them with:  while IFS=: read -r name pw uid gid gecos home shell; do ...; done < <(users)
users() {
	awk -F: 'NF == 7 && $1 != "" && $1 !~ /^#/' "$DATA/passwd"
}

# is_login_shell SHELL -- false for an empty shell and the usual "no login" shells
is_login_shell() {
	case $1 in
	"" | /usr/sbin/nologin | /sbin/nologin | /bin/false | /usr/bin/false) return 1 ;;
	esac
	return 0
}

# norm_path PATH -- PATH without trailing slashes ("/" stays "/")
norm_path() {
	local p=$1
	while [ "${#p}" -gt 1 ] && [ "${p%/}" != "$p" ]; do
		p=${p%/}
	done
	printf '%s\n' "$p"
}
