import os
import shutil
import subprocess

ROOT = os.getcwd()
SANDBOX = os.path.join(ROOT, "__checker__", "sandbox")
SCRIPT = os.path.join(ROOT, "audit.sh")

ED = "AAAAC3NzaC1lZDI1NTE5AAAAIGq7Rb2YkWm0x1sT3dVhPz8nL4cQe9uJfA5oK6iXyHbD"
RSA = "AAAAB3NzaC1yc2EAAAADAQABAAABAQDeLq2v8"


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except subprocess.TimeoutExpired:
        print(f"TEST {name} FAIL audit.sh did not finish within 30s")
    except Exception as e:
        print(f"TEST {name} FAIL {type(e).__name__}: {e}")


def pw(name, uid, gid=None, home=None, shell="/bin/bash"):
    gid = uid if gid is None else gid
    home = f"/home/{name}" if home is None else home
    return f"{name}:x:{uid}:{gid}:{name}:{home}:{shell}"


def audit(files, arg: str | None = "snap"):
    top = os.path.join(SANDBOX, "run")
    shutil.rmtree(top, ignore_errors=True)
    os.makedirs(os.path.join(top, "snap"))
    for rel, text in files.items():
        p = os.path.join(top, "snap", rel)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w") as fh:
            fh.write(text if text.endswith("\n") or text == "" else text + "\n")
    argv = ["bash", SCRIPT] + ([arg] if arg is not None else [])
    r = subprocess.run(argv, cwd=top, capture_output=True, text=True, timeout=30)
    return r.returncode, r.stdout, r.stderr


def lines_of(out, code):
    return [ln for ln in out.split("\n") if ln.startswith(f"[{code}]")]


def same(got, want, what):
    for i in range(max(len(got), len(want))):
        gl = got[i] if i < len(got) else "<missing>"
        wl = want[i] if i < len(want) else "<nothing more>"
        assert gl == wl, f"{what}: line {i + 1}: expected {wl!r}, got {gl!r}"


def check_code(files, code, want, what):
    rc, out, err = audit(files)
    assert rc in (0, 1), f"{what}: exit status {rc} (stderr: {err.strip()[:200]!r})"
    same(lines_of(out, code), want, f"{what} [{code}] lines")
    return out


def t_uid_checks():
    passwd = "\n".join([
        pw("root", 0, home="/root"), pw("ops", 0, home="/root"), pw("web", 1500, gid=1500),
        "#legacy:x:1500:1500:old:/home/legacy:/bin/bash", pw("ftp", 900, gid=1500, shell="/usr/sbin/nologin"),
        pw("app", 1500), pw("svc", 70), "broken:x:1500:1500:/home/broken", pw("worker", 1500),
        pw("a2", 2000), pw("a1", 2000), pw("lone", 999, gid=2000)])
    out = check_code({"passwd": passwd}, "UID0", ["[UID0] ops: uid 0"], "uid 0 users")
    same(lines_of(out, "DUP_UID"), ["[DUP_UID] uid 0: root, ops", "[DUP_UID] uid 1500: web, app, worker",
                                    "[DUP_UID] uid 2000: a2, a1"], "duplicate uids [DUP_UID] lines")


def t_aging_rules():
    passwd = "\n".join([pw("root", 0, home="/root"), pw("ann", 1000), pw("ben", 1001, shell="/bin/zsh"),
                        pw("cid", 1002), pw("dee", 1003), pw("eve", 1004, shell="/usr/sbin/nologin"),
                        pw("fay", 1005, shell="/bin/false"), pw("gus", 1006), pw("hal", 1007, shell=""),
                        pw("ivy", 1008), pw("jon", 1009, shell="/bin/sh")])
    shadow = "\n".join(["root:$6$r$abc:20000:0:90:7:::", "ann:$y$a$abc:20000:99999:90:7:::",
                        "ben:$y$b$abc:20000:0:99999:7:::", "cid:!$y$c$abc:20000:0::7:::",
                        "dee:*:20000:0:99999:7:::", "eve:$y$e$abc:20000:0:99999:7:::", "fay:$y$f$abc:20000:0::7:::",
                        "gus:$y$g$abc:20000:0::7:::", "hal:$y$h$abc:20000:0::7:::", "jon:$y$j$abc:20000:0:180:7:::"])
    check_code({"passwd": passwd, "shadow": shadow}, "NO_AGING", [
        "[NO_AGING] ben: login shell /bin/zsh, no maximum password age",
        "[NO_AGING] gus: login shell /bin/bash, no maximum password age",
        "[NO_AGING] ivy: login shell /bin/bash, no shadow entry"], "password aging")


def t_aging_without_shadow():
    passwd = "\n".join([pw("root", 0, home="/root"), pw("ann", 1000, shell="/sbin/nologin"), pw("ben", 1001)])
    check_code({"passwd": passwd}, "NO_AGING", [
        "[NO_AGING] root: login shell /bin/bash, no shadow entry",
        "[NO_AGING] ben: login shell /bin/bash, no shadow entry"], "no shadow file")


def homes_passwd():
    return "\n".join([pw("root", 0, home="/root"), pw("ann", 1000), pw("ben", 1001, home="/srv/ben/"),
                      pw("cid", 1002, home="/data/Shared Projects/cid"), pw("dee", 1003, shell="/usr/sbin/nologin"),
                      pw("eve", 1004), pw("fay", 1005), pw("gus", 1006), pw("hal", 1007, home="/home/hal//"),
                      pw("ivy", 1008)])


def homes_manifest():
    return "\n".join(["# mode owner path", "700 root /root", "0777 ann /home/ann", "755 ben /srv/ben",
                      "1777 root /data/Shared Projects/cid", "777 nobody /home/dee", "rwx eve /home/eve",
                      "750 eve /home/eve", "0702 fay /home/fay", "775 gus /home/gus", "770 hal /home/hal",
                      "2757 ivy /home/ivy", "755 ann /home/ivy"])


def t_world_writable():
    check_code({"passwd": homes_passwd(), "homes.manifest": homes_manifest()}, "WW_HOME", [
        "[WW_HOME] ann: /home/ann is world-writable (0777)",
        "[WW_HOME] cid: /data/Shared Projects/cid is world-writable (1777)",
        "[WW_HOME] dee: /home/dee is world-writable (777)",
        "[WW_HOME] fay: /home/fay is world-writable (0702)"], "world-writable homes")


def t_home_owner():
    check_code({"passwd": homes_passwd(), "homes.manifest": homes_manifest()}, "HOME_OWNER", [
        "[HOME_OWNER] cid: /data/Shared Projects/cid is owned by root",
        "[HOME_OWNER] ivy: /home/ivy is owned by ann"], "home owners")


def t_key_comments():
    passwd = "\n".join([pw("ann", 1000), pw("ben", 1001), pw("cid", 1002)])
    ann = "\n".join([
        f"ssh-ed25519 {ED} ann@laptop",
        "",
        "   # old laptop, removed 2026-01",
        f"ssh-ed25519 {ED}",
        f'from="10.0.0.0/8",command="backup --full --to /mnt/x" ssh-rsa {RSA} backup job',
        f'from="10.0.0.0/8",command="backup --full --to /mnt/x" ssh-rsa {RSA}',
        f"sk-ssh-ed25519@openssh.com {ED} yubikey",
        f"ecdsa-sha2-nistp384 {RSA}   ",
        f"\tssh-ed25519\t{ED}\tann@tab",
        f"no-pty,no-agent-forwarding ecdsa-sha2-nistp256 {RSA}",
    ])
    cid = f"restrict ssh-ed25519 {ED} cid@ci\n"
    check_code({"passwd": passwd, "ssh/ann.authorized_keys": ann, "ssh/cid.authorized_keys": cid,
                "ssh/zed.authorized_keys": f"ssh-rsa {RSA}\n"}, "KEY_NO_COMMENT", [
        "[KEY_NO_COMMENT] ann: authorized_keys line 4 (ssh-ed25519)",
        "[KEY_NO_COMMENT] ann: authorized_keys line 6 (ssh-rsa)",
        "[KEY_NO_COMMENT] ann: authorized_keys line 8 (ecdsa-sha2-nistp384)",
        "[KEY_NO_COMMENT] ann: authorized_keys line 10 (ecdsa-sha2-nistp256)"], "key comments")


def t_key_bad():
    passwd = "\n".join([pw("ann", 1000), pw("ben", 1001)])
    ben = "\n".join([
        f"ssh-ed25519 {ED} ben@desk",
        "ssh-ed25519",
        f"ssh-ed448 {ED} ben@future",
        "this is not a key",
        f'command="echo no type here" {ED}',
        f'from="a b" ssh-rsa {RSA} ok',
    ])
    check_code({"passwd": passwd, "ssh/ben.authorized_keys": ben, "ssh/ann.authorized_keys": "garbage\n"},
               "KEY_BAD", ["[KEY_BAD] ann: authorized_keys line 1 is not a valid key",
                           "[KEY_BAD] ben: authorized_keys line 2 is not a valid key",
                           "[KEY_BAD] ben: authorized_keys line 3 is not a valid key",
                           "[KEY_BAD] ben: authorized_keys line 4 is not a valid key",
                           "[KEY_BAD] ben: authorized_keys line 5 is not a valid key"], "invalid keys")


def sudo_snapshot():
    passwd = "\n".join([pw("root", 0, home="/root"), pw("ann", 1000), pw("ben", 1001, gid=3000), pw("cid", 1002),
                        pw("dee", 1003, gid=3001), pw("eve", 1004), pw("fay", 1005)])
    group = "\n".join(["root:x:0:", "wheel:x:10:ann,ghost,cid", "ops:x:3000:eve", "db:x:3001:", "empty:x:3002:"])
    sudoers = "\n".join([
        "Defaults env_reset",
        "root ALL=(ALL:ALL) ALL",
        "%wheel ALL=(ALL:ALL) NOPASSWD: ALL",
        "#includedir /etc/sudoers.d",
    ])
    return {"passwd": passwd, "group": group, "sudoers": sudoers}


def t_sudo_groups():
    files = sudo_snapshot()
    files["sudoers.d/50-ops"] = "\n".join([
        "# ops",
        "Cmnd_Alias SVC = /usr/bin/systemctl",
        "User_Alias ADMINS = fay",
        "   %ops ALL=(root) NOPASSWD: SVC",
        "%db,dee ALL=(postgres) NOPASSWD: /usr/bin/psql",
        "%empty ALL=(ALL) NOPASSWD: ALL",
        "%nogroup ALL=(ALL) NOPASSWD: ALL",
        "fay ALL=(ALL) ALL",
    ])
    check_code(files, "SUDO_NOPASSWD", [
        "[SUDO_NOPASSWD] ann: sudoers line 3 (%wheel)",
        "[SUDO_NOPASSWD] ben: sudoers.d/50-ops line 4 (%ops)",
        "[SUDO_NOPASSWD] cid: sudoers line 3 (%wheel)",
        "[SUDO_NOPASSWD] dee: sudoers.d/50-ops line 5 (%db)",
        "[SUDO_NOPASSWD] eve: sudoers.d/50-ops line 4 (%ops)"], "sudo via groups")


def t_sudo_dropins():
    files = sudo_snapshot()
    files["sudoers"] = "Defaults env_reset\n"
    files["sudoers.d/9-late"] = "eve ALL=(ALL) NOPASSWD: /bin/ls\n"
    files["sudoers.d/10-early"] = "# x\neve ALL=(ALL) NOPASSWD: /bin/cat\nann,ann ALL=(ALL) NOPASSWD: /bin/df\n"
    files["sudoers.d/Zeta"] = "fay ALL=(ALL) NOPASSWD: /bin/true\n"
    files["sudoers.d/eve.bak"] = "eve ALL=(ALL) NOPASSWD: ALL\n"
    files["sudoers.d/20-cid~"] = "cid ALL=(ALL) NOPASSWD: ALL\n"
    files["sudoers.d/30-nobody"] = "ghost ALL=(ALL) NOPASSWD: ALL\nann ALL=(ALL) PASSWD: ALL\n"
    check_code(files, "SUDO_NOPASSWD", [
        "[SUDO_NOPASSWD] ann: sudoers.d/10-early line 3 (direct)",
        "[SUDO_NOPASSWD] eve: sudoers.d/10-early line 2 (direct)",
        "[SUDO_NOPASSWD] eve: sudoers.d/9-late line 1 (direct)",
        "[SUDO_NOPASSWD] fay: sudoers.d/Zeta line 1 (direct)"], "sudoers.d drop-ins")


def t_sudo_dedup():
    files = sudo_snapshot()
    files["sudoers"] = "ann,%wheel,cid ALL=(ALL) NOPASSWD: /bin/ls\n%ops,ben ALL=(ALL) NOPASSWD: /bin/ps\n"
    check_code(files, "SUDO_NOPASSWD", [
        "[SUDO_NOPASSWD] ann: sudoers line 1 (direct)",
        "[SUDO_NOPASSWD] ben: sudoers line 2 (%ops)",
        "[SUDO_NOPASSWD] cid: sudoers line 1 (%wheel)",
        "[SUDO_NOPASSWD] eve: sudoers line 2 (%ops)"], "one finding per user and rule")


FULL = {
    "passwd": "\n".join([pw("root", 0, home="/root"), pw("daemon", 1, home="/usr/sbin", shell="/usr/sbin/nologin"),
                         pw("kim", 1100), pw("lee", 1101), pw("max", 1100, gid=1102, home="/home/max/"),
                         pw("nia", 1103, shell="/usr/sbin/nologin")]),
    "shadow": "\n".join(["root:$6$x$y:20000:0:90:7:::", "daemon:*:20000:0:99999:7:::", "kim:$y$k$z:20000:0:60:7:::",
                         "lee:$y$l$z:20000:7:99999:7:::", "max:$y$m$z:20000:0:90:7:::"]),
    "group": "root:x:0:\nadm:x:4:kim\nkim:x:1100:\nlee:x:1101:\nmax:x:1102:\n",
    "sudoers": "%adm ALL=(ALL) NOPASSWD: /usr/bin/journalctl\n",
    "sudoers.d/lee": "lee ALL=(ALL) ALL\n",
    "homes.manifest": "700 root /root\n750 kim /home/kim\n777 lee /home/lee\n750 kim /home/max\n1777 root /home/nia\n",
    "ssh/kim.authorized_keys": f"ssh-ed25519 {ED} kim@x\nssh-rsa {RSA}\n",
    "ssh/max.authorized_keys": "ssh-ed25519\n",
}


def t_full_report():
    rc, out, err = audit(FULL)
    same(out.rstrip("\n").split("\n"), [
        "[DUP_UID] uid 1100: kim, max",
        "[NO_AGING] lee: login shell /bin/bash, no maximum password age",
        "[WW_HOME] lee: /home/lee is world-writable (777)",
        "[WW_HOME] nia: /home/nia is world-writable (1777)",
        "[HOME_OWNER] max: /home/max/ is owned by kim",
        "[KEY_NO_COMMENT] kim: authorized_keys line 2 (ssh-rsa)",
        "[KEY_BAD] max: authorized_keys line 1 is not a valid key",
        "[SUDO_NOPASSWD] kim: sudoers line 1 (%adm)",
        "findings: 8"], "full report")
    assert rc == 1, f"full report: exit status {rc}, expected 1 when there are findings"


def t_clean_snapshot():
    files = {"passwd": pw("root", 0, home="/root") + "\n" + pw("kim", 1100),
             "shadow": "root:$6$x$y:20000:0:90:7:::\nkim:$y$k$z:20000:0:60:7:::\n",
             "sudoers.d/kim": "kim ALL=(ALL) ALL\n", "ssh/kim.authorized_keys": f"ssh-ed25519 {ED} kim@x\n"}
    rc, out, err = audit(files)
    same(out.rstrip("\n").split("\n"), ["findings: 0"], "clean snapshot")
    assert rc == 0, f"clean snapshot: exit status {rc}, expected 0"


def t_usage():
    rc, _, _ = audit({"passwd": pw("root", 0)}, arg=None)
    assert rc == 2, f"no DATA_DIR: expected exit 2, got {rc}"
    rc, _, _ = audit({"shadow": "root:x:1::::::"})
    assert rc == 2, f"DATA_DIR without passwd: expected exit 2, got {rc}"


TESTS = [("uid_checks", t_uid_checks), ("aging_rules", t_aging_rules), ("aging_without_shadow", t_aging_without_shadow),
         ("world_writable", t_world_writable), ("home_owner", t_home_owner), ("key_comments", t_key_comments),
         ("key_bad", t_key_bad), ("sudo_groups", t_sudo_groups), ("sudo_dropins", t_sudo_dropins),
         ("sudo_dedup", t_sudo_dedup), ("full_report", t_full_report), ("clean_snapshot", t_clean_snapshot),
         ("usage", t_usage)]

for name, fn in TESTS:
    case(name, fn)
