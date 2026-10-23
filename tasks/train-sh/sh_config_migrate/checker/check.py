import os
import shutil
import subprocess

ROOT = os.getcwd()
SANDBOX = os.path.join(ROOT, "__checker__", "sandbox")
SCRIPT = os.path.join(ROOT, "migrate.sh")

MAPPING = "\n".join([
    "# section.key\tnew.key\ttype",
    "server.host\thttp.bind\tstr",
    "server.port\thttp.port\tint",
    "server.debug\thttp.debug\tbool",
    "db.dsn\tdatabase.dsn\tstr",
    "db.retries\tdatabase.retries\tint",
    "",
    "global.name\tservice.name\tstr",
    "global.owner\tservice.owner\tstr",
    "app.level\tapp.log_level\tstr",
    "app.path\tapp.path\tstr",
    "app.flag\tapp.flag\tbool",
    "app.flag2\tapp.flag2\tbool",
    "app.flag3\tapp.flag3\tbool",
    "app.flag4\tapp.flag4\tbool",
    "app.offset\tapp.offset\tint",
]) + "\n"


def case(name, fn):
    try:
        fn()
        print(f"TEST {name} PASS")
    except AssertionError as e:
        print(f"TEST {name} FAIL {e}")
    except subprocess.TimeoutExpired:
        print(f"TEST {name} FAIL migrate.sh did not finish within 20s")
    except Exception as e:
        print(f"TEST {name} FAIL {type(e).__name__}: {e}")


class Run:
    def __init__(self, files, args=None, pre=None):
        self.dir = os.path.join(SANDBOX, "run")
        shutil.rmtree(self.dir, ignore_errors=True)
        os.makedirs(os.path.join(self.dir, "old"))
        with open(os.path.join(self.dir, "map.tsv"), "w") as fh:
            fh.write(MAPPING)
        for name, text in files.items():
            p = os.path.join(self.dir, "old", name)
            os.makedirs(os.path.dirname(p), exist_ok=True)
            with open(p, "w") as fh:
                fh.write(text)
        for name, text in (pre or {}).items():
            p = os.path.join(self.dir, "new", name)
            os.makedirs(os.path.dirname(p), exist_ok=True)
            with open(p, "w") as fh:
                fh.write(text)
        args = ["map.tsv", "old", "new"] if args is None else args
        r = subprocess.run(["bash", SCRIPT, *args], cwd=self.dir, capture_output=True, text=True, timeout=20)
        self.rc, self.out, self.err = r.returncode, r.stdout, r.stderr

    def conf(self, name):
        p = os.path.join(self.dir, "new", name)
        return open(p).read() if os.path.exists(p) else None

    def exists(self, name):
        return os.path.exists(os.path.join(self.dir, "new", name))


def same(got, want, what):
    g = got.rstrip("\n").split("\n") if got and got.strip() else []
    w = want.strip("\n").split("\n")
    for i in range(max(len(g), len(w))):
        gl = g[i] if i < len(g) else "<missing>"
        wl = w[i] if i < len(w) else "<nothing more>"
        assert gl == wl, f"{what}: line {i + 1}: expected {wl!r}, got {gl!r}"


def ok_run(files, what):
    r = Run(files)
    assert r.rc == 0, f"{what}: exit status {r.rc}, expected 0 (stdout: {r.out.strip()[-200:]!r})"
    return r


def conf_of(r, name, what):
    c = r.conf(name)
    assert c is not None, f"{what}: {name} was not written"
    return c


def t_basic_layout():
    r = ok_run({"web.ini": "name = web\n[server]\nport = 80\nhost = 10.0.0.5\n"}, "a simple config")
    same(conf_of(r, "web.conf", "simple config"),
         "# migrated from web.ini\nhttp.bind=10.0.0.5\nhttp.port=80\nservice.name=web", "web.conf")
    same(r.out, "migrated 1 of 1 files, 0 errors, 0 warnings", "report")


def t_comments_and_blanks():
    text = "; header comment\n# another\n\n   ; indented\n\t# tabbed\n[server]\n  ;port = 1\nport = 81\n   \n"
    r = ok_run({"c.ini": text}, "comments")
    same(conf_of(r, "c.conf", "comments"), "# migrated from c.ini\nhttp.port=81", "c.conf")


def t_inline_comments():
    text = ("[server]\nport = 8080 ; behind the LB\nhost = db#primary\n"
            "[app]\nlevel = info\t# tab before the hash\npath = ; nothing set yet\nflag = on;off\n")
    r = Run({"i.ini": text})
    assert r.rc == 1, f"inline comments: exit status {r.rc}, expected 1 ('on;off' is not a boolean)"
    same(r.out, "i.ini:7: error: app.flag: expected a boolean, got 'on;off'\n"
         "migrated 0 of 1 files, 1 errors, 0 warnings", "report")
    r = ok_run({"i.ini": text.replace("flag = on;off\n", "")}, "inline comments")
    same(conf_of(r, "i.conf", "inline comments"),
         "# migrated from i.ini\napp.log_level=info\napp.path=\nhttp.bind=db#primary\nhttp.port=8080", "i.conf")


def t_quoted_values():
    text = ('[db]\ndsn = "host=db port=5432 ; sslmode=require # primary"\n'
            "[app]\nlevel = 'debug'\npath = \"  /srv/app data  \" ; trailing comment\n"
            'name_unused = "#"\n')
    r = Run({"q.ini": text})
    assert r.rc == 0, f"quoted values: exit status {r.rc}, expected 0 (stdout: {r.out.strip()[-200:]!r})"
    same(conf_of(r, "q.conf", "quoted values"),
         "# migrated from q.ini\napp.log_level='debug'\napp.path=  /srv/app data  \n"
         "database.dsn=host=db port=5432 ; sslmode=require # primary\nlegacy.app.name_unused=#", "q.conf")


def t_duplicate_keys():
    text = "[server]\nport = 80\nhost = a\nPort = 8080\n[db]\nretries = 3\n[server]\nhost = b\n"
    r = ok_run({"d.ini": text}, "duplicate keys")
    same(conf_of(r, "d.conf", "duplicate keys"),
         "# migrated from d.ini\ndatabase.retries=3\nhttp.bind=b\nhttp.port=8080", "d.conf")
    same(r.out, "d.ini:4: warning: duplicate key server.port, earlier value replaced\n"
         "d.ini:8: warning: duplicate key server.host, earlier value replaced\n"
         "migrated 1 of 1 files, 0 errors, 2 warnings", "report")


def t_case_and_global():
    text = "Name = Billing API\nOWNER=ops@corp.example\n[ SERVER ]\nHost=0.0.0.0\n"
    r = ok_run({"g.ini": text}, "case and global keys")
    same(conf_of(r, "g.conf", "case and global keys"),
         "# migrated from g.ini\nhttp.bind=0.0.0.0\nservice.name=Billing API\nservice.owner=ops@corp.example", "g.conf")


def t_unmapped_keys():
    text = "[server]\nport = 80\nthreads = 8\n[tls]\ncert = /etc/tls/a.pem\n[Server]\nzone = eu\n"
    r = ok_run({"u.ini": text}, "unmapped keys")
    same(conf_of(r, "u.conf", "unmapped keys"),
         "# migrated from u.ini\nhttp.port=80\nlegacy.server.threads=8\nlegacy.server.zone=eu\n"
         "legacy.tls.cert=/etc/tls/a.pem", "u.conf")
    same(r.out, "u.ini:3: warning: unmapped key server.threads, kept as legacy.server.threads\n"
         "u.ini:5: warning: unmapped key tls.cert, kept as legacy.tls.cert\n"
         "u.ini:7: warning: unmapped key server.zone, kept as legacy.server.zone\n"
         "migrated 1 of 1 files, 0 errors, 3 warnings", "report")


def t_bool_values():
    text = "[server]\ndebug = Yes\n[app]\nflag = OFF\nflag2 = 1\nflag3 = False\nflag4 = On\n"
    r = ok_run({"b.ini": text}, "booleans")
    same(conf_of(r, "b.conf", "booleans"),
         "# migrated from b.ini\napp.flag=false\napp.flag2=true\napp.flag3=false\napp.flag4=true\nhttp.debug=true",
         "b.conf")
    r = Run({"b.ini": "[server]\ndebug = maybe\n"})
    assert r.rc == 1, f"debug = maybe: exit status {r.rc}, expected 1"
    same(r.out, "b.ini:2: error: server.debug: expected a boolean, got 'maybe'\n"
         "migrated 0 of 1 files, 1 errors, 0 warnings", "report")


def t_int_values():
    r = ok_run({"n.ini": "[app]\noffset = -15\n[db]\nretries = 007\n"}, "integers")
    same(conf_of(r, "n.conf", "integers"), "# migrated from n.ini\napp.offset=-15\ndatabase.retries=007", "n.conf")
    r = Run({"n.ini": "[server]\nport = 80a\n[db]\nretries = 2.5\n"})
    assert r.rc == 1, f"bad integers: exit status {r.rc}, expected 1"
    same(r.out, "n.ini:2: error: server.port: expected an integer, got '80a'\n"
         "n.ini:4: error: db.retries: expected an integer, got '2.5'\n"
         "migrated 0 of 1 files, 2 errors, 0 warnings", "report")
    assert not r.exists("n.conf"), "n.conf was written although n.ini has errors"


def t_parse_errors():
    text = '[server]\nport 80\n= nothing\nhost = "db-1\n[app]\nlevel = warn\nlevel = info\nextra = 1\n'
    r = Run({"p.ini": text})
    assert r.rc == 1, f"parse errors: exit status {r.rc}, expected 1"
    same(r.out, "p.ini:2: error: cannot parse line\np.ini:3: error: cannot parse line\n"
         "p.ini:4: error: unterminated quote\n"
         "p.ini:7: warning: duplicate key app.level, earlier value replaced\n"
         "p.ini:8: warning: unmapped key app.extra, kept as legacy.app.extra\n"
         "migrated 0 of 1 files, 3 errors, 2 warnings", "report")


def t_error_file_not_deployed():
    r = Run({"bad.ini": "[server]\nport = eighty\n", "good.ini": "[server]\nport = 80\n"},
            pre={"bad.conf": "# migrated from bad.ini\nhttp.port=79\n"})
    assert r.rc == 1, f"one broken file: exit status {r.rc}, expected 1"
    assert not r.exists("bad.conf"), "bad.conf from an earlier run is still there although bad.ini has an error"
    same(conf_of(r, "good.conf", "good file next to a broken one"), "# migrated from good.ini\nhttp.port=80", "good.conf")
    same(r.out.strip().split("\n")[-1], "migrated 1 of 2 files, 1 errors, 0 warnings", "summary line")


def t_file_selection():
    files = {"zeta.ini": "[server]\nport = 1\nport = 2\n", "alpha svc.ini": "[server]\nport = 3\nfoo = 1\n",
             "Beta.ini": "name = B\n", "notes.ini.bak": "[server]\nport = x\n", "sub/inner.ini": "[server]\nport = y\n",
             "readme.txt": "not a config\n"}
    r = ok_run(files, "a directory of configs")
    same(r.out, "alpha svc.ini:3: warning: unmapped key server.foo, kept as legacy.server.foo\n"
         "zeta.ini:3: warning: duplicate key server.port, earlier value replaced\n"
         "migrated 3 of 3 files, 0 errors, 2 warnings", "report")
    same(conf_of(r, "alpha svc.conf", "file name with a space"),
         "# migrated from alpha svc.ini\nhttp.port=3\nlegacy.server.foo=1", "alpha svc.conf")
    assert r.exists("Beta.conf") and r.exists("zeta.conf"), "Beta.conf / zeta.conf not written"
    assert not r.exists("inner.conf") and not r.exists("sub/inner.conf"), "a config in a subdirectory was migrated"
    assert not r.exists("notes.ini.conf") and not r.exists("notes.conf"), "notes.ini.bak was migrated"


def t_usage():
    r = Run({"a.ini": "name = a\n"}, args=["map.tsv", "old"])
    assert r.rc == 2, f"two arguments: exit status {r.rc}, expected 2"
    r = Run({"a.ini": "name = a\n"}, args=["map.tsv", "missing-dir", "new"])
    assert r.rc == 2, f"missing SRC_DIR: exit status {r.rc}, expected 2"
    r = Run({"a.ini": "name = a\n"}, args=["nomap.tsv", "old", "new"])
    assert r.rc == 2, f"unreadable mapping: exit status {r.rc}, expected 2"


TESTS = [("basic_layout", t_basic_layout), ("comments_and_blanks", t_comments_and_blanks),
         ("inline_comments", t_inline_comments), ("quoted_values", t_quoted_values),
         ("duplicate_keys", t_duplicate_keys), ("case_and_global", t_case_and_global),
         ("unmapped_keys", t_unmapped_keys), ("bool_values", t_bool_values), ("int_values", t_int_values),
         ("parse_errors", t_parse_errors), ("error_file_not_deployed", t_error_file_not_deployed),
         ("file_selection", t_file_selection), ("usage", t_usage)]

for name, fn in TESTS:
    case(name, fn)
