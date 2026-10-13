# Language layer: TypeScript, and the language servers for TypeScript, HTML,
# CSS and JSON. Built by `automaticum image build typescript` on the base.
FROM automaticum-lane:base

RUN npm install --global --ignore-scripts --no-audit --no-fund \
      typescript@7.0.2 typescript-language-server@6.0.2 vscode-langservers-extracted@4.10.0 \
 && npm cache clean --force
