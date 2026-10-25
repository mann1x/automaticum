# Lane layer of tbench2/feal-linear-cryptanalysis, written by `automaticum suite import tbench2`
# from Terminal-Bench 2.0 commit 2fd12b88aafdd04a52c298e3940bcb189f9766d6 (066 §3): the task's own image
# (alexgshaw/feal-linear-cryptanalysis:20251031, pinned by digest) with the runner added (064 §3.2) under its own
# prefix, /opt/automaticum, which is on no PATH (D113): Node and the runner,
# and a static ripgrep that only the SDK's own search finds. /app, where the
# task works, is made the workspace; nothing else of the image changes.
# Built by `automaticum image build tb2-feal-linear-cryptanalysis`; rebuild the base first.
FROM automaticum-lane:base AS lane
FROM alexgshaw/feal-linear-cryptanalysis@sha256:ea78749a8422c6228d26c8753f45906a6a2df089906764b7e3c3e27e8f718ee7

COPY --from=lane /usr/local/bin/node /opt/automaticum/bin/node
COPY --from=lane /opt/automaticum/tools/rg /opt/automaticum/bin/rg
COPY --from=lane /opt/automaticum/runner /opt/automaticum/runner
RUN ["/opt/automaticum/bin/node", "--version"]
RUN rm -rf /app && ln -s /workspace /app
WORKDIR /workspace
