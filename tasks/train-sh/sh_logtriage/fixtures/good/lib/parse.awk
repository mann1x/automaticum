# parse.awk - turn access-log lines into tab-separated records for logsum.sh
#
# Output, one line per parsed request:
#   R <iso-time> <hour> <method> <path> <status> <ms>
# and one final line with the counters:
#   C <lines> <parsed> <malformed>

BEGIN {
	months = "JanFebMarAprMayJunJulAugSepOctNovDec"
	lines = 0
	parsed = 0
	bad = 0
}

/^[ \t\r]*$/ { next }

{
	lines++
	if (parse_line($0))
		parsed++
	else
		bad++
}

END { printf "C\t%d\t%d\t%d\n", lines, parsed, bad }

function parse_line(line,    q, n, head, req, tail, ms, w, nw, t, d, mon, y, hh, mi, ss, m, path, st, ts) {
	n = split(line, q, "\"")
	if (n != 7)
		return 0
	head = q[1]
	req = q[2]
	tail = q[3]
	ms = q[7]
	if (head !~ /^[^ ]+ [^ ]+ [^ ]+ \[[0-9][0-9]\/[A-Z][a-z][a-z]\/[0-9][0-9][0-9][0-9]:[0-9][0-9]:[0-9][0-9]:[0-9][0-9] [-+][0-9][0-9][0-9][0-9]\] $/)
		return 0
	if (tail !~ /^ [0-9][0-9][0-9] ([0-9]+|-) $/)
		return 0
	if (ms !~ /^ [0-9]+$/)
		return 0
	nw = split(req, w, " ")
	if (nw != 3 || w[1] !~ /^[A-Z]+$/ || substr(w[2], 1, 1) != "/" || w[3] !~ /^HTTP\//)
		return 0

	t = substr(head, index(head, "[") + 1, 20)
	d = substr(t, 1, 2)
	mon = substr(t, 4, 3)
	y = substr(t, 8, 4)
	hh = substr(t, 13, 2)
	mi = substr(t, 16, 2)
	ss = substr(t, 19, 2)
	m = index(months, mon)
	if (m == 0 || (m - 1) % 3 != 0)
		return 0
	m = (m - 1) / 3 + 1

	path = w[2]
	sub(/\?.*/, "", path)
	split(tail, st, " ")
	ts = sprintf("%s-%02d-%sT%s:%s:%s", y, m, d, hh, mi, ss)
	printf "R\t%s\t%s\t%s\t%s\t%s\t%d\n", ts, substr(ts, 1, 13), w[1], path, st[1], ms + 0
	return 1
}
