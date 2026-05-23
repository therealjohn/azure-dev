# Foundry docs for AI agents (Preview)

Single front door for agent-friendly documentation across every
`azure.ai.*` extension. Each sibling extension owns its own embedded
markdown topics; this extension routes topic requests to the right
sibling and renders the result.

The shape mirrors a familiar `skills` surface:

```bash
# Top-level index -- which ai.* extensions have docs
azd ai doc

# List topics for the agents extension
azd ai doc agent

# Print one topic's markdown
azd ai doc agent initialize
azd ai doc agent configure
azd ai doc agent investigate
azd ai doc agent operate
```

Each topic is a contract an agent reads to drive the CLI: exact
invocations, JSON shape examples, error codes, confirmation-envelope
handling. Topics are owned by (and embedded in) the extension they
describe; this extension is a thin forwarder.

Today only `azure.ai.agents` ships docs. As other ai.* extensions adopt
the integration pattern, they get added to `agentSiblings` in
`internal/cmd/doc_index.go`.
