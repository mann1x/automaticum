# Lane layer of tbench2/regex-chess, written by `automaticum suite import tbench2`
# from Terminal-Bench 2.0 commit 2fd12b88aafdd04a52c298e3940bcb189f9766d6 (066 §3): the task's own image
# (alexgshaw/regex-chess:20251031, pinned by digest) with the runner added (064 §3.2) under its own
# prefix, /opt/automaticum, which is on no PATH (D113): Node and the runner,
# and a static ripgrep that only the SDK's own search finds. /app, where the
# task works, is made the workspace; nothing else of the image changes.
# Built by `automaticum image build tb2-regex-chess`; rebuild the base first.
FROM automaticum-lane:base AS lane
FROM alexgshaw/regex-chess@sha256:e3b6d61d2d3930fbbdc5f7bf091a7f8b7f6125e1951ef8a7dbe165b1ef62942c

COPY --from=lane /usr/local/bin/node /opt/automaticum/bin/node
COPY --from=lane /opt/automaticum/tools/rg /opt/automaticum/bin/rg
COPY --from=lane /opt/automaticum/runner /opt/automaticum/runner
RUN ["/opt/automaticum/bin/node", "--version"]
RUN rm -rf /app && ln -s /workspace /app
WORKDIR /workspace
