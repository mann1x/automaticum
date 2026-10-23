# Language layer: Python 3.12, the oracle image's own build (python:3.12
# on Debian 12, pinned by digest), copied onto the base. Standard library
# only: no pip packages. Built by `automaticum image build python`.
FROM python@sha256:14a4933e6cef421424f7a66d89a3f4eafcbd0d90e0a822251f298e05132dc735 AS py
FROM automaticum-lane:base

# The shared libraries that build links against.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      libbz2-1.0 libexpat1 libffi8 libgdbm6 liblzma5 libncursesw6 libreadline8 \
      libsqlite3-0 libssl3 libuuid1 zlib1g \
 && rm -rf /var/lib/apt/lists/*
COPY --from=py /usr/local/lib/python3.12 /usr/local/lib/python3.12
COPY --from=py /usr/local/lib/libpython3.12.so.1.0 /usr/local/lib/
COPY --from=py /usr/local/include/python3.12 /usr/local/include/python3.12
COPY --from=py /usr/local/bin/python3.12 /usr/local/bin/
RUN ln -s libpython3.12.so.1.0 /usr/local/lib/libpython3.12.so \
 && ln -s python3.12 /usr/local/bin/python3 && ln -s python3.12 /usr/local/bin/python \
 && ldconfig \
 && python3 -c "import ssl, sqlite3, ctypes, lzma, bz2, zlib, json, unittest"
ENV PYTHONDONTWRITEBYTECODE=1
