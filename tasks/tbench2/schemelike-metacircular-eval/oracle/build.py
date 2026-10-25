# Builds the verifier of a Terminal-Bench 2.0 task imported by automaticum
# (066 §3), as the task's artefact: a zipapp of main.py, the task's tests
# under tests/ and the pinned wheels unpacked under site/. Every entry has a
# fixed time and mode and the entries are sorted, so the same inputs give
# the same bytes.
#
#   python3 build.py <the task's oracle directory> <output>
import os
import sys
import zipfile

src, out = sys.argv[1], sys.argv[2]
entries = {}


def add(name, data, mode):
    if name in entries:
        sys.exit("build.py: %s is given twice" % name)
    entries[name] = (data, mode)


def read(path):
    with open(path, "rb") as f:
        return f.read()


add("__main__.py", read(os.path.join(src, "main.py")), 0o644)
tests = os.path.join(src, "tests")
for d, dirs, files in os.walk(tests):
    dirs.sort()
    for n in sorted(files):
        p = os.path.join(d, n)
        mode = 0o755 if os.stat(p).st_mode & 0o111 else 0o644
        add("tests/" + os.path.relpath(p, tests).replace(os.sep, "/"), read(p), mode)
wheels = os.path.join(src, "wheels")
for n in sorted(os.listdir(wheels)):
    with zipfile.ZipFile(os.path.join(wheels, n)) as w:
        for info in w.infolist():
            if not info.is_dir():
                add("site/" + info.filename, w.read(info), 0o644)

with zipfile.ZipFile(out, "w") as z:
    for name in sorted(entries):
        data, mode = entries[name]
        info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
        info.create_system = 3
        info.external_attr = (0o100000 | mode) << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        z.writestr(info, data)
