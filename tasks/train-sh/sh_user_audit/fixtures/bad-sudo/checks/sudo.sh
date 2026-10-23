# sudo.sh - SUDO_NOPASSWD (from sudoers and sudoers.d/)

check_sudo() {
	local -a files=()
	local f base
	[ -f "$DATA/sudoers" ] && files+=("$DATA/sudoers")
	if [ -d "$DATA/sudoers.d" ]; then
		while IFS= read -r f; do
			base=${f##*/}
			[ -f "$f" ] && files+=("$f")
		done < <(find "$DATA/sudoers.d" -mindepth 1 -maxdepth 1 | sort)
	fi
	[ "${#files[@]}" -gt 0 ] || return 0

	awk -v data="$DATA" '
		FILENAME == data "/passwd" {
			if (NF != 7 || $1 == "" || $1 ~ /^#/) next
			if (!($1 in order)) { order[$1] = ++nusers; uname[nusers] = $1; ugid[$1] = $4 }
			next
		}
		FILENAME == data "/group" {
			if ($1 == "" || $1 ~ /^#/) next
			gid[$1] = $3
			members[$1] = $4
			next
		}
		{
			where = substr(FILENAME, length(data) + 2)
			line = $0
			sub(/^[ \t]+/, "", line)
			if (line == "" || line ~ /^#/ || line ~ /^Defaults/ || line ~ /^(Cmnd|User|Host|Runas)_Alias/) next
			if (index(line, "NOPASSWD:") == 0) next
			split(line, tok, /[ \t]+/)
			np = split(tok[1], who, ",")
			delete seen
			for (i = 1; i <= np; i++) {
				p = who[i]
				if (substr(p, 1, 1) == "%") {
					g = substr(p, 2)
					if (!(g in gid)) continue
					nm = split(members[g], mem, ",")
					for (j = 1; j <= nm; j++) { m = mem[j]; gsub(/[ \t]/, "", m); add(m, where, FNR, p) }
					for (k = 1; k <= nusers; k++) if (ugid[uname[k]] == gid[g]) add(uname[k], where, FNR, p)
				} else {
					add(p, where, FNR, "direct")
				}
			}
		}
		function add(u, where, ln, how) {
			if (!(u in order) || (u in seen)) return
			seen[u] = 1
			nf_++
			printf "%d\t%d\t[SUDO_NOPASSWD] %s: %s line %d (%s)\n", order[u], nf_, u, where, ln, how
		}
	' FS=: "$DATA/passwd" FS=: "$( [ -f "$DATA/group" ] && echo "$DATA/group" || echo /dev/null)" FS=" " "${files[@]}" |
		sort -t "$(printf '\t')" -k1,1n -k2,2n | cut -f3-
}
