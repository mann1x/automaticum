# Lane layer of tbench2/schemelike-metacircular-eval, written by `automaticum suite import tbench2`
# from Terminal-Bench 2.0 commit 2fd12b88aafdd04a52c298e3940bcb189f9766d6 (066 §3): the task's own image
# (alexgshaw/schemelike-metacircular-eval:20251031, pinned by digest) with the runner added (064 §3.2) under its own
# prefix, /opt/automaticum, which is on no PATH (D113): Node and the runner,
# and a static ripgrep that only the SDK's own search finds. /app, where the
# task works, is made the workspace; nothing else of the image changes.
# Built by `automaticum image build tb2-schemelike-metacircular-eval`; rebuild the base first.
FROM automaticum-lane:base AS lane
FROM alexgshaw/schemelike-metacircular-eval@sha256:b485be38bf80d56c1a4cd9da95bbf7ec129dc03f6786b86c0525518a96f6d1db

COPY --from=lane /usr/local/bin/node /opt/automaticum/bin/node
COPY --from=lane /opt/automaticum/tools/rg /opt/automaticum/bin/rg
COPY --from=lane /opt/automaticum/runner /opt/automaticum/runner
RUN ["/opt/automaticum/bin/node", "--version"]
RUN rm -rf /app && ln -s /workspace /app
WORKDIR /workspace
