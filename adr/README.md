# Architecture Decisions

| # | Decision | In one line |
| --- | --- | --- |
| [0001](0001-start-with-a-local-review-service-and-isolated-language-serv.md) | Start with a local review service and isolated language servers | Local review service with credentials on the host and isolated revision workspaces. |
| [0002](0002-load-operator-defined-language-servers-through-isolated-stdi.md) | Load operator-defined language servers through isolated stdio profiles | Keep Go and Terraform defaults while allowing operator-defined language servers. |
| [0003](0003-connect-external-ai-clients-to-browser-review-sessions-throu.md) | Connect external AI clients to browser review sessions through MCP | Use MCP for revision-bound code inspection and acknowledged browser draft/navigation actions. |
| [0004](0004-prepare-pinned-go-dependencies-before-starting-the-offline-l.md) | Prepare pinned Go dependencies before starting the offline language server | Resolve checksum-pinned dependencies through a bounded fetch stage while retaining an offline language-server sandbox. |
| [0005](0005-isolate-public-reviews-by-browser-supplied-github-credential.md) | Isolate public reviews by browser supplied GitHub credential | Public hosting uses bounded credential-isolated workspaces and rechecks GitHub access. |
