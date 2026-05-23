# Agent-friendly documentation for `azure.ai.*` extensions

## Status

**Adopted** -- `azure.ai.docs` is the canonical front door for agent-friendly
documentation across every `azure.ai.*` extension as of `agent-ready` branch
(commits `2926d5d23`, `7b60c685b`). The first sibling onboarded is
`azure.ai.agents` with four topics (`initialize`, `configure`, `investigate`,
`operate`).

## Goal

Give an AI coding assistant a single, low-friction entry point to learn how
to drive any `azure.ai.*` extension on behalf of a developer, without
relying on training data, web fetches, or guesswork. The agent runs one
command, reads one slice of markdown, and is ready to act.

## Background

An [agent-friendly CLI playbook][playbook] (the source for this
work) identified three properties an agent needs from a CLI:

1. **Discoverable docs that ship with the binary.** Agents can't browse a
   docs site reliably; whatever the CLI's `--help` and a single docs
   command return is the world.
2. **Progressive disclosure.** Top-level lists categories; categories list
   topics; topics return focused markdown. Loading one topic should not
   pull the entire product manual into the context window.
3. **Self-sufficient topics.** Each topic must let the agent act on its
   own -- exact CLI invocations, expected JSON shapes, error codes,
   confirmation-protocol handling. No "see also" without inline coverage.

This document specifies how the `azure.ai.*` extension family delivers
those three properties.

[playbook]: https://www.youtube.com/watch?v=xE1FcJDuLpM

## Architecture

### One extension owns the content

All agent-friendly markdown for the `azure.ai.*` family lives in a single
dedicated extension: **`azure.ai.docs`**. Each sibling extension does NOT
ship its own docs surface. There are no `azd ai agent docs`,
`azd ai toolbox docs`, etc. commands.

Rationale:

| Approach | Result |
|---|---|
| Content in each sibling, `azd ai doc` shells out | `azd ai doc` is useless without every sibling installed. Hidden RPC coupling between extensions. Cross-extension release coordination. Tried first; rejected. |
| Content in `azure.ai.docs` (this doc) | One install, all docs work. Sibling extensions stay focused on their own command surface. No cross-extension RPC. |
| Content in the website only | Agents can't reliably browse. Doc version drifts from CLI version on a per-machine basis. |

### Command surface

The command surface mirrors a familiar `skills` shape:

```
azd ai doc                            # Index: list ai.* extensions with docs
azd ai doc <category>                 # List topic names for one category
azd ai doc <category> <topic>         # Print one topic as markdown
```

Concrete example for the agents category:

```bash
azd ai doc                            # -> "Foundry agents (azure.ai.agents)  azd ai doc agent"
azd ai doc agent                      # -> configure, initialize, investigate, operate
azd ai doc agent initialize           # -> full markdown body
azd ai doc agent nonexistent          # -> "Valid topics: configure, initialize, ..."
```

Output is plain markdown to stdout. The body is the same thing a contributor
would write in a tutorial: it's intended to be read directly by an AI
coding assistant or piped into one.

### Filesystem layout

```
cli/azd/extensions/azure.ai.docs/
  extension.yaml                            # id: azure.ai.docs, namespace: ai.doc
  internal/cmd/
    root.go                                 # `azd ai doc` root + RunE -> index
    doc_index.go                            # docCategories table + runDocIndex
    doc_agent.go                            # `azd ai doc agent [topic]`
    skills/
      agent/                                # category folder = subcommand name
        initialize.md
        configure.md
        investigate.md
        operate.md
      toolbox/                              # future: topics for azure.ai.toolboxes
        ...
      project/                              # future: topics for azure.ai.projects
        ...
```

The category folder name MUST match the corresponding subcommand name in
`root.go` and the `SubcommandName` field of the `docCategory` entry in
`doc_index.go`. The match is enforced by convention; `printCategoryTopic`
uses the subcommand name as the directory key when reading from the
embedded FS.

### Embedding mechanism

`internal/cmd/doc_agent.go` declares:

```go
//go:embed skills/*/*.md
var skillsFS embed.FS
```

The two-deep glob covers every category and every topic. Adding a new
topic is mechanical: drop a `.md` file under `skills/<category>/` and
rebuild. No manifest, no registration code.

The embed path is relative to the file containing the directive, so the
markdown MUST stay under `internal/cmd/skills/` -- moving it elsewhere
silently breaks the build.

### Reserved-flag contract

The docs extension does NOT register a local `--output` flag. `--output`
is a reserved azd global. Topic format is markdown by default; structured
JSON output, if ever needed, must be added via `azdext.RegisterFlagOptions`
(SDK-controlled) rather than `cmd.Flags().StringVar`. The agents extension
hit this gotcha during the rollout (commit `b28ae56fd`) -- pin a regression
test if you re-introduce an `--output` flag on a docs leaf.

## Topic authoring guidelines

Each topic markdown is a **contract** the agent reads to drive the CLI.
Treat it like API documentation, not like a tutorial: dense, command-heavy,
no marketing.

### The four canonical topics

The first onboarded category (`agent`) uses these four. Other categories
should follow the same shape unless there's a strong reason not to:

| Topic | Owns | Does NOT own |
|---|---|---|
| `initialize` | The bootstrap path. Identity verification, project detection, init, provision, deploy. The "what do I run first" answer. | Operating the deployed thing. |
| `configure` | Shaping the resource before deployment. Manifest editing, connection management, file uploads, endpoint config. Idempotent updates. | Billed runs. Destructive ops. |
| `investigate` | Read-only inspection. `show`, `list`, log streaming, doctor, JSON shapes for state queries. | Mutations of any kind. |
| `operate` | Mutations, billed jobs, destructive ops. The confirmation envelope contract. Recovery from failures. | Bootstrap. Read-only inspection. |

A category that doesn't have a billed-runs story can drop `operate`; a
category that's read-only can drop `operate` and `configure`.

### What every topic MUST contain

1. **Audience line at the top.** Who is reading this -- usually "an AI
   coding assistant driving [extension] on behalf of a developer". Sets
   the tone for everything below.
2. **Exact CLI invocations.** Every command shown must be runnable
   verbatim. Use placeholders like `<topic-id>` only where the value
   genuinely varies; otherwise inline a representative value.
3. **`--output json` shape per command.** Show one success payload and
   (when relevant) one error / not-found / not-deployed payload. Keys
   are part of the wire contract.
4. **Error code coverage.** List the `code` field values an agent is
   likely to see from this workflow, with one-line remediation.
5. **Confirmation envelope handling.** Any topic that surfaces write
   commands MUST show the `{status: "confirmation_required", ...}`
   envelope shape and the three rules: present `description`+`changes`
   to the human, never auto-`--force`, run `confirmCommand` verbatim.
6. **Cross-references to sibling topics, not to web URLs.** "See the
   `investigate` topic for read paths" beats "see the docs site".

### What every topic MUST NOT contain

- Marketing copy about why the product exists.
- Long prose explanations -- prefer command + example + JSON shape.
- "TODO" or "coming soon" placeholders. If a command isn't shipping
  yet, omit it from the doc.
- Inlined large secret/example values. Reference env vars by name.

### Length

5-8 KB per topic is a healthy target. Above 10 KB suggests the topic
should be split (e.g. `operate-eval.md`, `operate-optimize.md`). Below
2 KB suggests the topic could be folded into a sibling.

## Adding a new sibling category

Three mechanical steps:

1. **Drop markdown.** Create `internal/cmd/skills/<sibling>/<topic>.md`
   files. Follow the topic authoring guidelines above.
2. **Register the category** in `internal/cmd/doc_index.go`:
   ```go
   var docCategories = []docCategory{
       {SubcommandName: "agent",   DisplayName: "Foundry agents (azure.ai.agents)"},
       {SubcommandName: "toolbox", DisplayName: "Foundry toolboxes (azure.ai.toolboxes)"}, // new
   }
   ```
3. **Wire a subcommand** in `internal/cmd/root.go`:
   ```go
   rootCmd.AddCommand(newToolboxCommand())
   ```
   and create `internal/cmd/doc_toolbox.go` modeled on `doc_agent.go`.
   The function bodies are nearly identical -- just substitute the
   category name in the calls to `listCategoryTopics` /
   `printCategoryTopic`.

That's the entire onboarding. No coordination with the sibling extension
is required.

When a sibling adds new write commands, its agent-friendly story is to
update the relevant topic markdown in `azure.ai.docs`, NOT to add a
local docs command. The pull request author for the sibling typically
also updates the matching topic file.

## Discoverability

### From the sibling extension's `--help`

The reference implementation in `azure.ai.agents` shows the recommended
pattern: every sibling extension's root `--help` includes a "DOCS &
AGENT SKILLS" section pointing at the docs front door:

```
DOCS & AGENT SKILLS
  Inspect state, identity, and health from the terminal:

    azd ai agent show --output json                Inspect the deployed agent record.
    azd ai agent project show --output json        Inspect identity, subscription, project context.
    azd ai agent doctor --output json              Diagnose configuration, auth, and deployment issues.

  Agent-friendly workflow docs (install the azure.ai.docs extension):

    azd ext install azure.ai.docs                  One-time install of the docs front door.
    azd ai doc                                     List ai.* extensions with docs available.
    azd ai doc agent                               List skill topics for this extension.
    azd ai doc agent <topic>                       Print one topic (initialize, configure, ...).
```

Each sibling extension should add an equivalent block to its own
`--help` so that running just the sibling's `--help` immediately
surfaces the docs path. See `azure.ai.agents/internal/cmd/help_output.go`
(the `docsAndAgentSkillsSection` function) for the reference
implementation.

### From the agent itself

A coding assistant typically discovers `azd ai doc agent` by:

- Running `azd ai agent --help` and reading the DOCS section above.
- Running the install hint and then `azd ai doc agent` to list topics.
- Loading one topic into context and acting on it.

A sibling extension's agent-friendly story is incomplete until both the
docs topics exist in `azure.ai.docs` and the `--help` pointer exists in
the sibling extension.

## Relationship with the sibling CLI surface

Topic markdown is downstream of the actual CLI behavior. Before writing
a topic, the sibling extension MUST have:

- **`--output json` on every data-returning leaf** the topic references.
- **`--dry-run` and `--force` on every write command** the topic references.
- **The confirmation-envelope contract** (exit 2 + JSON) on every write
  command in agent mode.
- **Structured errors with stable `code` fields** so the topic's "common
  error codes" section reflects what the CLI actually returns.

A topic that documents flags the CLI doesn't actually support is worse
than no topic. See `cli/azd/extensions/azure.ai.agents/AGENTS.md` for
the agents-extension implementation of these prerequisites.

## Local development

```bash
cd cli/azd/extensions/azure.ai.docs

# First-time install (the manifest + binary together)
azd x build
azd x pack
azd x publish
azd ext install azure.ai.docs

# Subsequent iteration (binary only; manifest already in ~/.azd/extensions/)
azd x watch
```

`azd x build` alone deploys only the binary to
`~/.azd/extensions/azure.ai.docs/`, not the `extension.yaml` manifest.
Without the manifest the azd host won't register the command surface, so
`azd ai doc` will silently not appear under `azd ai`. The pack/publish
flow is what writes the manifest, so the first install in any environment
needs the full sequence.

## Out of scope / non-goals

- **No MCP server.** Topic markdown is the action contract; the CLI is
  the action surface. MCP would re-encode the same information into tool
  schemas an agent re-parses on every turn, with no offsetting benefit.
- **No remote fetch.** Topics are embedded at build time. An extension
  install pins both the CLI behavior and the docs that describe it.
- **No per-sibling `docs` command.** Tried first; rejected because it
  makes the docs extension useless on its own. See the rationale table
  in the Architecture section.
- **No structured-output mode on `azd ai doc agent <topic>`.** Topic
  bodies are markdown and only markdown. Agents are expected to consume
  the markdown directly; if structured output ever becomes necessary,
  add it via `azdext.RegisterFlagOptions` so the reserved-flag contract
  is honored.
- **No automatic mirroring of topics into the website.** Topic content
  optimizes for an agent reader, not a human one. A human-facing docs
  site can reference the topic source files but should not auto-publish
  them.

## References

- Reference implementation extension: `cli/azd/extensions/azure.ai.docs/`
- First onboarded sibling: `cli/azd/extensions/azure.ai.agents/`
- Confirmation envelope contract: agents `README.md` (Confirmation
  envelope for write commands section)
- Implementation commits on `agent-ready` branch:
  - `2926d5d23` -- initial `azure.ai.docs` extension scaffolding
  - `7b60c685b` -- moved content from agents into the docs extension
  - `b28ae56fd` -- reserved-flag fix (use SDK `RegisterFlagOptions`)
