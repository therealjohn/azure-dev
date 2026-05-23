# Foundry docs for AI agents (Preview)

Single front door for agent-friendly documentation across every
`azure.ai.*` extension. The markdown is embedded in this extension --
install once, and `azd ai doc <category> <topic>` returns documentation
for any covered ai.* extension without requiring the sibling extension
to be installed.

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

Each topic is a contract an agent reads to drive the matching CLI
commands: exact invocations, JSON shape examples, error codes,
confirmation-envelope handling.

## Adding topics for another ai.* extension

The repo layout is intentionally simple:

```
internal/cmd/
  skills/
    agent/            <-- topics for azure.ai.agents
      initialize.md
      configure.md
      investigate.md
      operate.md
    toolbox/          <-- future: topics for azure.ai.toolboxes
      ...
    project/          <-- future: topics for azure.ai.projects
      ...
  doc_index.go        <-- docCategories table (one entry per skills/ subdir)
  doc_agent.go        <-- per-extension subcommand
```

To add a new sibling:

1. Drop `skills/<sibling>/<topic>.md` files into this extension.
2. Add an entry to `docCategories` in `doc_index.go`.
3. Add a `new<Sibling>Command()` constructor mirroring `newAgentCommand()`
   and register it in `root.go`.

No coordination with the sibling extension is required; this extension is
the source of truth for its agent-friendly docs.
