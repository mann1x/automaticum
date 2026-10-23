# account audit

Security asked for a quarterly account audit of every host. We collect a **snapshot** of the
relevant files from each host into a directory, and `audit.sh` reads only that snapshot: it never
looks at the live system and never changes anything.

```
bash audit.sh DATA_DIR
```

`audit.sh` is the driver; each check lives in `checks/*.sh` and prints its findings on stdout;
`lib/common.sh` has shared helpers. `checks/uid.sh` and `checks/aging.sh` exist (QA says they are
not quite right yet); `checks/homes.sh`, `checks/keys.sh` and `checks/sudo.sh` are still empty.

Environment: bash 5, GNU coreutils, grep, sed, find, and `awk` is **mawk** (no gawk extensions, no
`{n}` regex intervals). `jq` and `sqlite3` are not installed.

## The snapshot (`DATA_DIR`)

| file | content |
|---|---|
| `passwd` | `/etc/passwd` (required) |
| `shadow` | `/etc/shadow` (may be missing) |
| `group` | `/etc/group` (may be missing) |
| `sudoers`, `sudoers.d/*` | `/etc/sudoers` and the drop-in directory (either may be missing) |
| `homes.manifest` | `MODE OWNER PATH` per line, from `stat -c '%a %U %n'`; `#` lines are comments (may be missing) |
| `ssh/<user>.authorized_keys` | each user's `~/.ssh/authorized_keys` (only for users that have one) |

A `passwd` line counts only if it has exactly 7 colon-separated fields, a non-empty name, and does
not start with `#`; other lines are skipped. "Passwd order" below means the order of those lines.
A user has a **login shell** unless the shell field is empty, `/usr/sbin/nologin`, `/sbin/nologin`,
`/bin/false` or `/usr/bin/false`.

## Checks

Each finding is one line `[CODE] SUBJECT: DETAIL`.

| code | finding | order within the code |
|---|---|---|
| `UID0` | `[UID0] NAME: uid 0` — a user other than `root` with uid 0 | passwd order |
| `DUP_UID` | `[DUP_UID] uid UID: NAME, NAME...` — a uid used by more than one user; the names in passwd order | uid, numerically |
| `NO_AGING` | a user with a login shell and no password aging, see below | passwd order |
| `WW_HOME` | `[WW_HOME] NAME: HOME is world-writable (MODE)` | passwd order |
| `HOME_OWNER` | `[HOME_OWNER] NAME: HOME is owned by OWNER` | passwd order |
| `KEY_NO_COMMENT` | `[KEY_NO_COMMENT] NAME: authorized_keys line N (KEYTYPE)` | passwd order, then line |
| `KEY_BAD` | `[KEY_BAD] NAME: authorized_keys line N is not a valid key` | passwd order, then line |
| `SUDO_NOPASSWD` | `[SUDO_NOPASSWD] NAME: FILE line N (HOW)` | passwd order, then file, then line |

The report lists all `UID0` findings, then all `DUP_UID`, ... in the table's order (the driver
already does this grouping), then `findings: N`. Exit status: 0 when there are no findings, 1 when
there are, 2 on bad usage or when `DATA_DIR/passwd` is missing.

### NO_AGING

For every user with a login shell:
- no line for the user in `shadow` (or no `shadow` file): `[NO_AGING] NAME: login shell SHELL, no shadow entry`;
- the password field (2nd) starts with `!` or `*` (locked, cannot log in with a password): no finding;
- the maximum password age (5th field) is empty or `99999`:
  `[NO_AGING] NAME: login shell SHELL, no maximum password age`.

### WW_HOME and HOME_OWNER

A user's home (6th passwd field) is looked up in `homes.manifest`. Paths are compared without
trailing slashes (`/srv/deploy/` and `/srv/deploy` are the same); `PATH` is the rest of the line
after `MODE` and `OWNER`, so it may contain spaces. Lines whose `MODE` is not 3-4 octal digits are
skipped; if a path is listed twice, the last line wins. A home that is not listed gives no finding.
- `WW_HOME` (any user): the mode's last digit has the write bit for others (2, 3, 6 or 7; `1777`
  counts). `MODE` is printed as written in the manifest, `HOME` as written in `passwd`.
- `HOME_OWNER` (users with a login shell only): the listed owner is not the user.

### KEY_NO_COMMENT and KEY_BAD

For each user (passwd order) that has `ssh/NAME.authorized_keys`, every line except blank lines and
`#` comments (leading whitespace allowed) is a key: `[OPTIONS] KEYTYPE BASE64 [COMMENT]`.
- `KEYTYPE` is one of `ssh-rsa`, `ssh-dss`, `ssh-ed25519`, `ecdsa-sha2-nistp256`,
  `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `sk-ssh-ed25519@openssh.com`,
  `sk-ecdsa-sha2-nistp256@openssh.com`.
- `OPTIONS` is present when the line does not start with a key type. It is one word, but double
  quoted parts of it may contain spaces: `from="10.0.0.0/8",command="backup --full" ssh-rsa AAAA...`.
- No comment after the base64 blob: `KEY_NO_COMMENT`. No key type, or no blob after it: `KEY_BAD`.
- Key files of names that are not in `passwd` are ignored.

### SUDO_NOPASSWD

Sudo rules are read from `sudoers`, then from the files in `sudoers.d/` in byte order of their
names, **skipping names that contain a `.` or end in `~`** (sudo itself ignores those). Line numbers
are per file; `FILE` is printed as `sudoers` or `sudoers.d/NAME`.
- Ignored lines: blank, starting with `#` (including `#include`/`#includedir`), `Defaults...`, and
  alias definitions (`Cmnd_Alias`, `User_Alias`, `Host_Alias`, `Runas_Alias`). Leading whitespace is
  allowed. Rules are one line each (no `\` continuations).
- A rule is `WHO HOSTS=... COMMANDS`; it is a finding when the line contains `NOPASSWD:`.
- `WHO` is a comma-separated list of user names and `%group` names (no spaces). A user name gives
  `(direct)`; `%group` gives `(%group)` for every member of the group: the names listed in the group's
  4th field in `group`, **and** every user whose primary gid (4th passwd field) is the group's gid.
- Only users that exist in `passwd` are reported. A user matched more than once by the same rule is
  reported once, with the first principal in `WHO` that matched.

## Example

`bash audit.sh sample` must print exactly this and exit 1:

```
[UID0] toor: uid 0
[DUP_UID] uid 0: root, toor
[DUP_UID] uid 1001: bob, backup
[NO_AGING] bob: login shell /bin/zsh, no maximum password age
[NO_AGING] deploy: login shell /bin/bash, no maximum password age
[NO_AGING] backup: login shell /bin/sh, no shadow entry
[WW_HOME] bob: /home/bob is world-writable (777)
[HOME_OWNER] carol: /home/carol is owned by alice
[HOME_OWNER] toor: /root is owned by root
[KEY_NO_COMMENT] alice: authorized_keys line 2 (ssh-ed25519)
[KEY_BAD] deploy: authorized_keys line 3 is not a valid key
[SUDO_NOPASSWD] bob: sudoers.d/90-ops line 3 (%ops)
[SUDO_NOPASSWD] deploy: sudoers.d/90-ops line 3 (%ops)
[SUDO_NOPASSWD] deploy: sudoers.d/95-deploy line 1 (direct)
findings: 14
```
