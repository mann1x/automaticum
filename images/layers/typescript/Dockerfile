# Language layer: TypeScript, and the language servers for TypeScript, HTML,
# CSS and JSON on the agent's PATH, as the layer always had them. The
# runner's own servers, for an arm with [agent.lsp] (D149), are the base's,
# under /opt/automaticum/lsp. Built by `automaticum image build typescript`
# on the base.
FROM automaticum-lane:base

RUN npm install --global --ignore-scripts --no-audit --no-fund \
      typescript@7.0.2 typescript-language-server@6.0.2 vscode-langservers-extracted@4.10.0 \
 && npm cache clean --force
