# keys.sh - KEY_NO_COMMENT and KEY_BAD (from ssh/<user>.authorized_keys)

check_keys() {
	local name pw uid gid gecos home shell file
	while IFS=: read -r name pw uid gid gecos home shell; do
		file="$DATA/ssh/$name.authorized_keys"
		[ -f "$file" ] || continue
		awk -v user="$name" '
			function after_options(s,    i, c, inq) {
				inq = 0
				for (i = 1; i <= length(s); i++) {
					c = substr(s, i, 1)
					if (c == "\"")
						inq = !inq
					else if (!inq && (c == " " || c == "\t"))
						return substr(s, i)
				}
				return ""
			}
			BEGIN {
				split("ssh-rsa ssh-dss ssh-ed25519 ecdsa-sha2-nistp256 ecdsa-sha2-nistp384 ecdsa-sha2-nistp521 sk-ssh-ed25519@openssh.com sk-ecdsa-sha2-nistp256@openssh.com", t, " ")
				for (i in t) known[t[i]] = 1
			}
			{
				line = $0
				sub(/\r$/, "", line)
				sub(/^[ \t]+/, "", line)
				if (line == "" || line ~ /^#/)
					next
				split(line, first, /[ \t]+/)
				if (!(first[1] in known)) {
					line = after_options(line)
					sub(/^[ \t]+/, "", line)
				}
				n = split(line, f, /[ \t]+/)
				if (n > 0 && f[n] == "")
					n--
				if (n < 2 || !(f[1] in known)) {
					printf "[KEY_BAD] %s: authorized_keys line %d is not a valid key\n", user, FNR
					next
				}
				if (n < 3)
					printf "[KEY_NO_COMMENT] %s: authorized_keys line %d (%s)\n", user, FNR, f[1]
			}' "$file"
	done < <(users)
}
