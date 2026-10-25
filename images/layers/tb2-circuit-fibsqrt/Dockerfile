# Lane layer of tbench2/circuit-fibsqrt, written by `automaticum suite import tbench2`
# from Terminal-Bench 2.0 commit 2fd12b88aafdd04a52c298e3940bcb189f9766d6 (066 §3): the task's own image
# (alexgshaw/circuit-fibsqrt:20251031, pinned by digest) with what the runner needs added (064 §3.2) and
# nothing else: Node and the runner from the base, git and ripgrep (the SDK's
# tools call them), and the lane's user, uid 1000. /app, where the task
# works, is made the workspace.
# Built by `automaticum image build tb2-circuit-fibsqrt`; rebuild the base first.
FROM automaticum-lane:base AS lane
FROM alexgshaw/circuit-fibsqrt@sha256:29783439f529eaed2145592f15af7a2281161528860392a382c825489030ae3a

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ripgrep \
 && rm -rf /var/lib/apt/lists/* \
 && if ! getent passwd 1000 >/dev/null; then \
      groupadd --gid 1000 node && useradd --uid 1000 --gid node --shell /bin/bash --create-home node; \
    fi
COPY --from=lane /usr/local/bin/node /usr/local/bin/
COPY --from=lane /opt/automaticum /opt/automaticum
RUN rm -rf /app && ln -s /workspace /app && node --version
WORKDIR /workspace
