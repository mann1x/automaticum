const SPEC = {
 "env": {
  "PYTHONDONTWRITEBYTECODE": "1"
 },
 "files": {
  "__checker__/check.py": "import os\nimport shutil\nimport subprocess\n\nROOT = os.getcwd()\nSANDBOX = os.path.join(ROOT, \"__checker__\", \"sandbox\")\nSCRIPT = os.path.join(ROOT, \"audit.sh\")\n\nED = \"AAAAC3NzaC1lZDI1NTE5AAAAIGq7Rb2YkWm0x1sT3dVhPz8nL4cQe9uJfA5oK6iXyHbD\"\nRSA = \"AAAAB3NzaC1yc2EAAAADAQABAAABAQDeLq2v8\"\n\n\ndef case(name, fn):\n    try:\n        fn()\n        print(f\"TEST {name} PASS\")\n    except AssertionError as e:\n        print(f\"TEST {name} FAIL {e}\")\n    except subprocess.TimeoutExpired:\n        print(f\"TEST {name} FAIL audit.sh did not finish within 30s\")\n    except Exception as e:\n        print(f\"TEST {name} FAIL {type(e).__name__}: {e}\")\n\n\ndef pw(name, uid, gid=None, home=None, shell=\"/bin/bash\"):\n    gid = uid if gid is None else gid\n    home = f\"/home/{name}\" if home is None else home\n    return f\"{name}:x:{uid}:{gid}:{name}:{home}:{shell}\"\n\n\ndef audit(files, arg: str | None = \"snap\"):\n    top = os.path.join(SANDBOX, \"run\")\n    shutil.rmtree(top, ignore_errors=True)\n    os.makedirs(os.path.join(top, \"snap\"))\n    for rel, text in files.items():\n        p = os.path.join(top, \"snap\", rel)\n        os.makedirs(os.path.dirname(p), exist_ok=True)\n        with open(p, \"w\") as fh:\n            fh.write(text if text.endswith(\"\\n\") or text == \"\" else text + \"\\n\")\n    argv = [\"bash\", SCRIPT] + ([arg] if arg is not None else [])\n    r = subprocess.run(argv, cwd=top, capture_output=True, text=True, timeout=30)\n    return r.returncode, r.stdout, r.stderr\n\n\ndef lines_of(out, code):\n    return [ln for ln in out.split(\"\\n\") if ln.startswith(f\"[{code}]\")]\n\n\ndef same(got, want, what):\n    for i in range(max(len(got), len(want))):\n        gl = got[i] if i < len(got) else \"<missing>\"\n        wl = want[i] if i < len(want) else \"<nothing more>\"\n        assert gl == wl, f\"{what}: line {i + 1}: expected {wl!r}, got {gl!r}\"\n\n\ndef check_code(files, code, want, what):\n    rc, out, err = audit(files)\n    assert rc in (0, 1), f\"{what}: exit status {rc} (stderr: {err.strip()[:200]!r})\"\n    same(lines_of(out, code), want, f\"{what} [{code}] lines\")\n    return out\n\n\ndef t_uid_checks():\n    passwd = \"\\n\".join([\n        pw(\"root\", 0, home=\"/root\"), pw(\"ops\", 0, home=\"/root\"), pw(\"web\", 1500, gid=1500),\n        \"#legacy:x:1500:1500:old:/home/legacy:/bin/bash\", pw(\"ftp\", 900, gid=1500, shell=\"/usr/sbin/nologin\"),\n        pw(\"app\", 1500), pw(\"svc\", 70), \"broken:x:1500:1500:/home/broken\", pw(\"worker\", 1500),\n        pw(\"a2\", 2000), pw(\"a1\", 2000), pw(\"lone\", 999, gid=2000)])\n    out = check_code({\"passwd\": passwd}, \"UID0\", [\"[UID0] ops: uid 0\"], \"uid 0 users\")\n    same(lines_of(out, \"DUP_UID\"), [\"[DUP_UID] uid 0: root, ops\", \"[DUP_UID] uid 1500: web, app, worker\",\n                                    \"[DUP_UID] uid 2000: a2, a1\"], \"duplicate uids [DUP_UID] lines\")\n\n\ndef t_aging_rules():\n    passwd = \"\\n\".join([pw(\"root\", 0, home=\"/root\"), pw(\"ann\", 1000), pw(\"ben\", 1001, shell=\"/bin/zsh\"),\n                        pw(\"cid\", 1002), pw(\"dee\", 1003), pw(\"eve\", 1004, shell=\"/usr/sbin/nologin\"),\n                        pw(\"fay\", 1005, shell=\"/bin/false\"), pw(\"gus\", 1006), pw(\"hal\", 1007, shell=\"\"),\n                        pw(\"ivy\", 1008), pw(\"jon\", 1009, shell=\"/bin/sh\")])\n    shadow = \"\\n\".join([\"root:$6$r$abc:20000:0:90:7:::\", \"ann:$y$a$abc:20000:99999:90:7:::\",\n                        \"ben:$y$b$abc:20000:0:99999:7:::\", \"cid:!$y$c$abc:20000:0::7:::\",\n                        \"dee:*:20000:0:99999:7:::\", \"eve:$y$e$abc:20000:0:99999:7:::\", \"fay:$y$f$abc:20000:0::7:::\",\n                        \"gus:$y$g$abc:20000:0::7:::\", \"hal:$y$h$abc:20000:0::7:::\", \"jon:$y$j$abc:20000:0:180:7:::\"])\n    check_code({\"passwd\": passwd, \"shadow\": shadow}, \"NO_AGING\", [\n        \"[NO_AGING] ben: login shell /bin/zsh, no maximum password age\",\n        \"[NO_AGING] gus: login shell /bin/bash, no maximum password age\",\n        \"[NO_AGING] ivy: login shell /bin/bash, no shadow entry\"], \"password aging\")\n\n\ndef t_aging_without_shadow():\n    passwd = \"\\n\".join([pw(\"root\", 0, home=\"/root\"), pw(\"ann\", 1000, shell=\"/sbin/nologin\"), pw(\"ben\", 1001)])\n    check_code({\"passwd\": passwd}, \"NO_AGING\", [\n        \"[NO_AGING] root: login shell /bin/bash, no shadow entry\",\n        \"[NO_AGING] ben: login shell /bin/bash, no shadow entry\"], \"no shadow file\")\n\n\ndef homes_passwd():\n    return \"\\n\".join([pw(\"root\", 0, home=\"/root\"), pw(\"ann\", 1000), pw(\"ben\", 1001, home=\"/srv/ben/\"),\n                      pw(\"cid\", 1002, home=\"/data/Shared Projects/cid\"), pw(\"dee\", 1003, shell=\"/usr/sbin/nologin\"),\n                      pw(\"eve\", 1004), pw(\"fay\", 1005), pw(\"gus\", 1006), pw(\"hal\", 1007, home=\"/home/hal//\"),\n                      pw(\"ivy\", 1008)])\n\n\ndef homes_manifest():\n    return \"\\n\".join([\"# mode owner path\", \"700 root /root\", \"0777 ann /home/ann\", \"755 ben /srv/ben\",\n                      \"1777 root /data/Shared Projects/cid\", \"777 nobody /home/dee\", \"rwx eve /home/eve\",\n                      \"750 eve /home/eve\", \"0702 fay /home/fay\", \"775 gus /home/gus\", \"770 hal /home/hal\",\n                      \"2757 ivy /home/ivy\", \"755 ann /home/ivy\"])\n\n\ndef t_world_writable():\n    check_code({\"passwd\": homes_passwd(), \"homes.manifest\": homes_manifest()}, \"WW_HOME\", [\n        \"[WW_HOME] ann: /home/ann is world-writable (0777)\",\n        \"[WW_HOME] cid: /data/Shared Projects/cid is world-writable (1777)\",\n        \"[WW_HOME] dee: /home/dee is world-writable (777)\",\n        \"[WW_HOME] fay: /home/fay is world-writable (0702)\"], \"world-writable homes\")\n\n\ndef t_home_owner():\n    check_code({\"passwd\": homes_passwd(), \"homes.manifest\": homes_manifest()}, \"HOME_OWNER\", [\n        \"[HOME_OWNER] cid: /data/Shared Projects/cid is owned by root\",\n        \"[HOME_OWNER] ivy: /home/ivy is owned by ann\"], \"home owners\")\n\n\ndef t_key_comments():\n    passwd = \"\\n\".join([pw(\"ann\", 1000), pw(\"ben\", 1001), pw(\"cid\", 1002)])\n    ann = \"\\n\".join([\n        f\"ssh-ed25519 {ED} ann@laptop\",\n        \"\",\n        \"   # old laptop, removed 2026-01\",\n        f\"ssh-ed25519 {ED}\",\n        f'from=\"10.0.0.0/8\",command=\"backup --full --to /mnt/x\" ssh-rsa {RSA} backup job',\n        f'from=\"10.0.0.0/8\",command=\"backup --full --to /mnt/x\" ssh-rsa {RSA}',\n        f\"sk-ssh-ed25519@openssh.com {ED} yubikey\",\n        f\"ecdsa-sha2-nistp384 {RSA}   \",\n        f\"\\tssh-ed25519\\t{ED}\\tann@tab\",\n        f\"no-pty,no-agent-forwarding ecdsa-sha2-nistp256 {RSA}\",\n    ])\n    cid = f\"restrict ssh-ed25519 {ED} cid@ci\\n\"\n    check_code({\"passwd\": passwd, \"ssh/ann.authorized_keys\": ann, \"ssh/cid.authorized_keys\": cid,\n                \"ssh/zed.authorized_keys\": f\"ssh-rsa {RSA}\\n\"}, \"KEY_NO_COMMENT\", [\n        \"[KEY_NO_COMMENT] ann: authorized_keys line 4 (ssh-ed25519)\",\n        \"[KEY_NO_COMMENT] ann: authorized_keys line 6 (ssh-rsa)\",\n        \"[KEY_NO_COMMENT] ann: authorized_keys line 8 (ecdsa-sha2-nistp384)\",\n        \"[KEY_NO_COMMENT] ann: authorized_keys line 10 (ecdsa-sha2-nistp256)\"], \"key comments\")\n\n\ndef t_key_bad():\n    passwd = \"\\n\".join([pw(\"ann\", 1000), pw(\"ben\", 1001)])\n    ben = \"\\n\".join([\n        f\"ssh-ed25519 {ED} ben@desk\",\n        \"ssh-ed25519\",\n        f\"ssh-ed448 {ED} ben@future\",\n        \"this is not a key\",\n        f'command=\"echo no type here\" {ED}',\n        f'from=\"a b\" ssh-rsa {RSA} ok',\n    ])\n    check_code({\"passwd\": passwd, \"ssh/ben.authorized_keys\": ben, \"ssh/ann.authorized_keys\": \"garbage\\n\"},\n               \"KEY_BAD\", [\"[KEY_BAD] ann: authorized_keys line 1 is not a valid key\",\n                           \"[KEY_BAD] ben: authorized_keys line 2 is not a valid key\",\n                           \"[KEY_BAD] ben: authorized_keys line 3 is not a valid key\",\n                           \"[KEY_BAD] ben: authorized_keys line 4 is not a valid key\",\n                           \"[KEY_BAD] ben: authorized_keys line 5 is not a valid key\"], \"invalid keys\")\n\n\ndef sudo_snapshot():\n    passwd = \"\\n\".join([pw(\"root\", 0, home=\"/root\"), pw(\"ann\", 1000), pw(\"ben\", 1001, gid=3000), pw(\"cid\", 1002),\n                        pw(\"dee\", 1003, gid=3001), pw(\"eve\", 1004), pw(\"fay\", 1005)])\n    group = \"\\n\".join([\"root:x:0:\", \"wheel:x:10:ann,ghost,cid\", \"ops:x:3000:eve\", \"db:x:3001:\", \"empty:x:3002:\"])\n    sudoers = \"\\n\".join([\n        \"Defaults env_reset\",\n        \"root ALL=(ALL:ALL) ALL\",\n        \"%wheel ALL=(ALL:ALL) NOPASSWD: ALL\",\n        \"#includedir /etc/sudoers.d\",\n    ])\n    return {\"passwd\": passwd, \"group\": group, \"sudoers\": sudoers}\n\n\ndef t_sudo_groups():\n    files = sudo_snapshot()\n    files[\"sudoers.d/50-ops\"] = \"\\n\".join([\n        \"# ops\",\n        \"Cmnd_Alias SVC = /usr/bin/systemctl\",\n        \"User_Alias ADMINS = fay\",\n        \"   %ops ALL=(root) NOPASSWD: SVC\",\n        \"%db,dee ALL=(postgres) NOPASSWD: /usr/bin/psql\",\n        \"%empty ALL=(ALL) NOPASSWD: ALL\",\n        \"%nogroup ALL=(ALL) NOPASSWD: ALL\",\n        \"fay ALL=(ALL) ALL\",\n    ])\n    check_code(files, \"SUDO_NOPASSWD\", [\n        \"[SUDO_NOPASSWD] ann: sudoers line 3 (%wheel)\",\n        \"[SUDO_NOPASSWD] ben: sudoers.d/50-ops line 4 (%ops)\",\n        \"[SUDO_NOPASSWD] cid: sudoers line 3 (%wheel)\",\n        \"[SUDO_NOPASSWD] dee: sudoers.d/50-ops line 5 (%db)\",\n        \"[SUDO_NOPASSWD] eve: sudoers.d/50-ops line 4 (%ops)\"], \"sudo via groups\")\n\n\ndef t_sudo_dropins():\n    files = sudo_snapshot()\n    files[\"sudoers\"] = \"Defaults env_reset\\n\"\n    files[\"sudoers.d/9-late\"] = \"eve ALL=(ALL) NOPASSWD: /bin/ls\\n\"\n    files[\"sudoers.d/10-early\"] = \"# x\\neve ALL=(ALL) NOPASSWD: /bin/cat\\nann,ann ALL=(ALL) NOPASSWD: /bin/df\\n\"\n    files[\"sudoers.d/Zeta\"] = \"fay ALL=(ALL) NOPASSWD: /bin/true\\n\"\n    files[\"sudoers.d/eve.bak\"] = \"eve ALL=(ALL) NOPASSWD: ALL\\n\"\n    files[\"sudoers.d/20-cid~\"] = \"cid ALL=(ALL) NOPASSWD: ALL\\n\"\n    files[\"sudoers.d/30-nobody\"] = \"ghost ALL=(ALL) NOPASSWD: ALL\\nann ALL=(ALL) PASSWD: ALL\\n\"\n    check_code(files, \"SUDO_NOPASSWD\", [\n        \"[SUDO_NOPASSWD] ann: sudoers.d/10-early line 3 (direct)\",\n        \"[SUDO_NOPASSWD] eve: sudoers.d/10-early line 2 (direct)\",\n        \"[SUDO_NOPASSWD] eve: sudoers.d/9-late line 1 (direct)\",\n        \"[SUDO_NOPASSWD] fay: sudoers.d/Zeta line 1 (direct)\"], \"sudoers.d drop-ins\")\n\n\ndef t_sudo_dedup():\n    files = sudo_snapshot()\n    files[\"sudoers\"] = \"ann,%wheel,cid ALL=(ALL) NOPASSWD: /bin/ls\\n%ops,ben ALL=(ALL) NOPASSWD: /bin/ps\\n\"\n    check_code(files, \"SUDO_NOPASSWD\", [\n        \"[SUDO_NOPASSWD] ann: sudoers line 1 (direct)\",\n        \"[SUDO_NOPASSWD] ben: sudoers line 2 (%ops)\",\n        \"[SUDO_NOPASSWD] cid: sudoers line 1 (%wheel)\",\n        \"[SUDO_NOPASSWD] eve: sudoers line 2 (%ops)\"], \"one finding per user and rule\")\n\n\nFULL = {\n    \"passwd\": \"\\n\".join([pw(\"root\", 0, home=\"/root\"), pw(\"daemon\", 1, home=\"/usr/sbin\", shell=\"/usr/sbin/nologin\"),\n                         pw(\"kim\", 1100), pw(\"lee\", 1101), pw(\"max\", 1100, gid=1102, home=\"/home/max/\"),\n                         pw(\"nia\", 1103, shell=\"/usr/sbin/nologin\")]),\n    \"shadow\": \"\\n\".join([\"root:$6$x$y:20000:0:90:7:::\", \"daemon:*:20000:0:99999:7:::\", \"kim:$y$k$z:20000:0:60:7:::\",\n                         \"lee:$y$l$z:20000:7:99999:7:::\", \"max:$y$m$z:20000:0:90:7:::\"]),\n    \"group\": \"root:x:0:\\nadm:x:4:kim\\nkim:x:1100:\\nlee:x:1101:\\nmax:x:1102:\\n\",\n    \"sudoers\": \"%adm ALL=(ALL) NOPASSWD: /usr/bin/journalctl\\n\",\n    \"sudoers.d/lee\": \"lee ALL=(ALL) ALL\\n\",\n    \"homes.manifest\": \"700 root /root\\n750 kim /home/kim\\n777 lee /home/lee\\n750 kim /home/max\\n1777 root /home/nia\\n\",\n    \"ssh/kim.authorized_keys\": f\"ssh-ed25519 {ED} kim@x\\nssh-rsa {RSA}\\n\",\n    \"ssh/max.authorized_keys\": \"ssh-ed25519\\n\",\n}\n\n\ndef t_full_report():\n    rc, out, err = audit(FULL)\n    same(out.rstrip(\"\\n\").split(\"\\n\"), [\n        \"[DUP_UID] uid 1100: kim, max\",\n        \"[NO_AGING] lee: login shell /bin/bash, no maximum password age\",\n        \"[WW_HOME] lee: /home/lee is world-writable (777)\",\n        \"[WW_HOME] nia: /home/nia is world-writable (1777)\",\n        \"[HOME_OWNER] max: /home/max/ is owned by kim\",\n        \"[KEY_NO_COMMENT] kim: authorized_keys line 2 (ssh-rsa)\",\n        \"[KEY_BAD] max: authorized_keys line 1 is not a valid key\",\n        \"[SUDO_NOPASSWD] kim: sudoers line 1 (%adm)\",\n        \"findings: 8\"], \"full report\")\n    assert rc == 1, f\"full report: exit status {rc}, expected 1 when there are findings\"\n\n\ndef t_clean_snapshot():\n    files = {\"passwd\": pw(\"root\", 0, home=\"/root\") + \"\\n\" + pw(\"kim\", 1100),\n             \"shadow\": \"root:$6$x$y:20000:0:90:7:::\\nkim:$y$k$z:20000:0:60:7:::\\n\",\n             \"sudoers.d/kim\": \"kim ALL=(ALL) ALL\\n\", \"ssh/kim.authorized_keys\": f\"ssh-ed25519 {ED} kim@x\\n\"}\n    rc, out, err = audit(files)\n    same(out.rstrip(\"\\n\").split(\"\\n\"), [\"findings: 0\"], \"clean snapshot\")\n    assert rc == 0, f\"clean snapshot: exit status {rc}, expected 0\"\n\n\ndef t_usage():\n    rc, _, _ = audit({\"passwd\": pw(\"root\", 0)}, arg=None)\n    assert rc == 2, f\"no DATA_DIR: expected exit 2, got {rc}\"\n    rc, _, _ = audit({\"shadow\": \"root:x:1::::::\"})\n    assert rc == 2, f\"DATA_DIR without passwd: expected exit 2, got {rc}\"\n\n\nTESTS = [(\"uid_checks\", t_uid_checks), (\"aging_rules\", t_aging_rules), (\"aging_without_shadow\", t_aging_without_shadow),\n         (\"world_writable\", t_world_writable), (\"home_owner\", t_home_owner), (\"key_comments\", t_key_comments),\n         (\"key_bad\", t_key_bad), (\"sudo_groups\", t_sudo_groups), (\"sudo_dropins\", t_sudo_dropins),\n         (\"sudo_dedup\", t_sudo_dedup), (\"full_report\", t_full_report), (\"clean_snapshot\", t_clean_snapshot),\n         (\"usage\", t_usage)]\n\nfor name, fn in TESTS:\n    case(name, fn)\n"
 },
 "hidden_dir": "__checker__",
 "steps": [
  {
   "cmd": [
    "bash",
    "-c",
    "for f in audit.sh lib/*.sh checks/*.sh; do bash -n \"$f\" || exit 1; done"
   ],
   "label": "syntax",
   "timeout_s": 30
  },
  {
   "cmd": [
    "python3",
    "-B",
    "__checker__/check.py"
   ],
   "label": "tests",
   "tests": true,
   "timeout_s": 240
  }
 ],
 "tests": [
  "uid_checks",
  "aging_rules",
  "aging_without_shadow",
  "world_writable",
  "home_owner",
  "key_comments",
  "key_bad",
  "sudo_groups",
  "sudo_dropins",
  "sudo_dedup",
  "full_report",
  "clean_snapshot",
  "usage"
 ]
};
// The checker every training task ships as `run_tests`: the agent may run it, and cannot read it.
//
// make_task.py prepends `const SPEC = {...};` (the task's hidden test files, its build and test
// steps, the test names it must see) and build.mjs (manic_miner's) turns the result into a Node
// single executable whose source is masked.
//
// It copies the working directory to a private temporary directory, writes the hidden tests into
// it, runs the steps there with the language's own tools, and prints ONE JSON line:
//
//   {"ok":true|false,"stage":"build"|"test"|"done","passed":N,"total":N,"failures":[...],"output":"..."}
//
// Test programs report one line per test on stdout:  TEST <name> PASS   or   TEST <name> FAIL <message>
// "ok" needs every step to exit 0 and every name in SPEC.tests to report PASS: a test that never
// reports (the program died, or exited early) is a failure. Output lines that point into the hidden
// test directory are dropped, so a build error never prints the tests' source.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const cp = require("node:child_process");

const HIDDEN = SPEC.hidden_dir || "__checker__";
const MAX_OUT = 6000;

function copyTree(src, dst) {
	fs.mkdirSync(dst, { recursive: true });
	for (const e of fs.readdirSync(src, { withFileTypes: true })) {
		if (e.name === ".git" || e.name === "run_tests" || e.name === HIDDEN) continue;
		const s = path.join(src, e.name);
		const d = path.join(dst, e.name);
		if (e.isDirectory()) copyTree(s, d);
		else if (e.isFile()) fs.copyFileSync(s, d);
	}
}

// A failure message is the test's own words, but an error inside it (a stack, a module path) can name a
// hidden file: any token that contains the hidden directory's name is replaced.
function hide(text) {
	return String(text).replace(new RegExp(`\\S*${HIDDEN}\\S*`, "g"), "<checker>");
}

// A compiler names the file on one line and quotes its source on the next ones ("  12 | code", a "^~~~"
// marker line): after a line that names a hidden file, those quoted lines are dropped as well.
const QUOTED = /^\s*(\d+\s*)?\|/;
const MARKER = /^\s*\|?\s*[\^~\-+]+[\s\^~\-+]*$/;

function scrub(text) {
	const lines = [];
	let quoting = false;
	for (const l of String(text || "").split("\n")) {
		if (l.includes(HIDDEN)) {
			quoting = true;
			continue;
		}
		if (quoting && (QUOTED.test(l) || MARKER.test(l) || /^\s*$/.test(l))) continue;
		quoting = false;
		if (!/^TEST /.test(l)) lines.push(l);
	}
	const out = lines.join("\n").trim();
	return out.length > MAX_OUT ? `${out.slice(0, MAX_OUT)}\n[... output cut]` : out;
}

function finish(res, work) {
	try {
		fs.rmSync(work, { recursive: true, force: true });
	} catch {}
	process.stdout.write(`${JSON.stringify(res)}\n`);
	process.exit(0);
}

const work = fs.mkdtempSync(path.join(os.tmpdir(), "chk-"));
const res = { ok: false, stage: "build", passed: 0, total: SPEC.tests.length, failures: [], output: "" };
try {
	copyTree(process.cwd(), work);
	for (const [rel, body] of Object.entries(SPEC.files)) {
		const p = path.join(work, rel);
		fs.mkdirSync(path.dirname(p), { recursive: true });
		fs.writeFileSync(p, body);
	}
} catch (e) {
	res.output = `could not prepare the check: ${e.message}`;
	finish(res, work);
}

const env = {
	PATH: process.env.PATH || "/usr/local/bin:/usr/bin:/bin",
	HOME: work,
	TMPDIR: work,
	LANG: "C.UTF-8",
	...(SPEC.env || {}),
};
for (const [k, v] of Object.entries(env)) env[k] = String(v).replaceAll("$WORK", work);

const seen = new Map();
let output = "";
for (const step of SPEC.steps) {
	const cmd = step.cmd.map((a) => String(a).replaceAll("$WORK", work));
	const r = cp.spawnSync(cmd[0], cmd.slice(1), {
		cwd: work,
		env,
		encoding: "utf8",
		timeout: (step.timeout_s || 120) * 1000,
		maxBuffer: 64 * 1024 * 1024,
	});
	const text = `${r.stdout || ""}\n${r.stderr || ""}`;
	if (step.tests) {
		res.stage = "test";
		for (const m of String(r.stdout || "").matchAll(/^TEST (\S+) (PASS|FAIL)(?: (.*))?$/gm)) {
			if (!seen.has(m[1]) || m[2] === "FAIL") seen.set(m[1], m[2] === "PASS" ? null : hide(m[3] || "failed").slice(0, 300));
		}
	}
	output += `${scrub(text)}\n`;
	if (r.error && r.error.code === "ETIMEDOUT") {
		res.output = scrub(`${output}\nstep timed out after ${step.timeout_s || 120}s: ${step.label || step.cmd[0]}`);
		break;
	}
	if (r.error) {
		res.output = scrub(`${output}\ncould not run ${cmd[0]}: ${r.error.message}`);
		break;
	}
	if (r.status !== 0 && !step.tests) {
		res.output = scrub(`${output}\n${step.label || step.cmd[0]} failed (exit ${r.status})`);
		break;
	}
	if (step.tests && r.status !== 0 && seen.size === 0) {
		res.output = scrub(`${output}\nthe tests did not run (exit ${r.status})`);
		break;
	}
}
for (const name of SPEC.tests) {
	if (!seen.has(name)) res.failures.push({ test: name, message: "did not run" });
	else if (seen.get(name) !== null) res.failures.push({ test: name, message: seen.get(name) });
	else res.passed += 1;
}
if (res.stage === "test" && !res.output) {
	res.stage = "done";
	res.output = scrub(output);
}
res.ok = res.stage === "done" && res.passed === res.total;
finish(res, work);
