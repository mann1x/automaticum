# ini2flat.awk - read one old-style INI file and emit tagged lines for migrate.sh
#
#   awk -v mapfile=MAPPING -f ini2flat.awk FILE.ini
#
# Output:
#   K <new.key=value>                         one per key that goes into the new file
#   F <line> <seq> <error|warning> <message>  one per finding

function trim(s) {
	sub(/^[ \t]+/, "", s)
	sub(/[ \t]+$/, "", s)
	return s
}

function finding(line, level, msg) {
	seq++
	printf "F\t%d\t%d\t%s\t%s\n", line, seq, level, msg
}

BEGIN {
	while ((getline row < mapfile) > 0) {
		if (row ~ /^[ \t]*(#|$)/)
			continue
		n = split(row, m, "\t")
		if (n < 3)
			continue
		newkey[m[1]] = m[2]
		type[m[1]] = m[3]
	}
	close(mapfile)
	sect = "global"
	seq = 0
}

{
	sub(/\r$/, "")
	t = trim($0)
	if (t == "" || t ~ /^[;#]/)
		next
	if (t ~ /^\[.*\]$/) {
		sect = tolower(trim(substr(t, 2, length(t) - 2)))
		next
	}
	eq = index(t, "=")
	key = tolower(trim(substr(t, 1, eq - 1)))
	if (eq == 0 || key == "") {
		finding(FNR, "error", "cannot parse line")
		next
	}
	val = trim(substr(t, eq + 1))
	if (val ~ /^[;#]/)
		val = ""
	else if (match(val, /[ \t][;#]/))
		val = trim(substr(val, 1, RSTART - 1))
	if (substr(val, 1, 1) == "\"") {
		rest = substr(val, 2)
		q = index(rest, "\"")
		if (q == 0) {
			finding(FNR, "error", "unterminated quote")
			next
		}
		val = substr(rest, 1, q - 1)
	}
	full = sect "." key
	if (full in vals)
		finding(FNR, "warning", "duplicate key " full ", earlier value replaced")
	vals[full] = val
	where[full] = FNR
}

END {
	for (full in vals) {
		v = vals[full]
		if (!(full in newkey)) {
			finding(where[full], "warning", "unmapped key " full ", kept as legacy." full)
			print "K\tlegacy." full "=" v
			continue
		}
		ty = type[full]
		if (ty == "int" && v !~ /^-?[0-9]+$/) {
			finding(where[full], "error", full ": expected an integer, got '" v "'")
			continue
		}
		if (ty == "bool") {
			lv = tolower(v)
			if (lv == "true" || lv == "yes" || lv == "on" || lv == "1")
				v = "true"
			else if (lv == "false" || lv == "no" || lv == "off" || lv == "0")
				v = "false"
			else {
				finding(where[full], "error", full ": expected a boolean, got '" v "'")
				continue
			}
		}
		print "K\t" newkey[full] "=" v
	}
}
