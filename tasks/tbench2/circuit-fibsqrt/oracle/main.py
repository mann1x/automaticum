# The verifier of a Terminal-Bench 2.0 task imported by automaticum (066 §3),
# run by the harness as the task's oracle after the agent stopped. It runs the
# task's own tests with pytest, as the task's tests/test.sh does, with no
# network: pytest and the tests' packages are in this file, pinned.
#
# The harness mounts the workspace as the agent left it at /w, read-only,
# and this file at /oracle; /app and /tests are empty and writable. The
# tests look for their files at those absolute paths.
#
#   python3 /oracle/verify.pyz <pytest's arguments>
import os
import shutil
import stat
import sys
import zipfile

SITE = "/tmp/automaticum-verifier"


def extract(z, prefix, dest):
    for info in z.infolist():
        if not info.filename.startswith(prefix) or info.is_dir():
            continue
        path = os.path.join(dest, info.filename[len(prefix):])
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with z.open(info) as src, open(path, "wb") as dst:
            shutil.copyfileobj(src, dst)
        os.chmod(path, ((info.external_attr >> 16) & 0o777) | 0o600)


def writable(path):
    for d, dirs, files in os.walk(path):
        for n in dirs + files:
            p = os.path.join(d, n)
            if not os.path.islink(p):
                os.chmod(p, os.stat(p).st_mode | stat.S_IWUSR)


def main():
    with zipfile.ZipFile(sys.argv[0]) as z:
        extract(z, "site/", SITE)
        extract(z, "tests/", "/tests")
    # Entry by entry: /app itself is the mount's, not ours to change.
    for name in sorted(os.listdir("/w")):
        src, dst = os.path.join("/w", name), os.path.join("/app", name)
        if os.path.isdir(src) and not os.path.islink(src):
            shutil.copytree(src, dst, symlinks=True)
        else:
            shutil.copy2(src, dst, follow_symlinks=False)
    writable("/app")
    os.chdir("/app")
    sys.path.insert(0, SITE)
    import pytest

    # pytest's arguments are the task's, as its tests/test.sh gives them.
    sys.exit(int(pytest.main(sys.argv[1:] + ["-p", "no:cacheprovider"])))


main()
