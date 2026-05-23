# Agent-friendly documentation for `azure.ai.*` extensions

## Status

**Adopted** -- `azure.ai.docs` is the canonical front door for agent-friendly
documentation across every `azure.ai.*` extension as of `agent-ready` branch.

Two surfaces are now in scope:

| Surface | Audience | Lives in | First implementation |
|---|---|---|---|
| **Read-only topics** (`azd ai doc <category> <topic>`) | An AI assistant fetching just-in-time docs while it works | `azure.ai.docs/internal/cmd/skills/<category>/<topic>.md` | `agent` category, four topics (`initialize`, `configure`, `investigate`, `operate`). Commits `2926d5d23`, `7b60c685b`. |
| **Installable skill packs** (`azd ai doc skills install --target <tool>`) plus the **agent-driven onboarding pre-flow** (`azd ai agent init` opening prompts) | A developer who wants their coding agent to drive setup from inside their own editor | `azure.ai.docs/internal/cmd/skills/SKILL.md` and `azure.ai.agents/internal/cmd/init_preflow.go` + `starter_prompts/` | `azd-ai-skill` pack + `azd ai agent init` pre-flow. Commits `0841bd3a5`, `99f46dc22`, `1021a3f05`, `a29867cae`, `a2637a8be`, `4d2b0b00f`. |

Both surfaces ship from `azure.ai.docs`; the pre-flow lives in the sibling
extension that triggers it but dispatches into `azure.ai.docs` for the
actual install.

## Goal

Give an AI coding assistant a single, low-friction entry point to learn how
to drive any `azure.ai.*` extension on behalf of a developer, without
relying on training data, web fetches, or guesswork. The agent runs one
command, reads one slice of markdown, and is ready to act.

A second goal added in the Phase 7 work: when a developer would rather have
their coding agent drive the *whole* setup (not just answer ad-hoc
questions), `azd ai agent init` opens with a three-question pre-flow that
installs the right skill pack for their tool, hands them a starter prompt
to paste, and gets out of the way. The developer's agent then drives
`azd init` / `provision` / `deploy` etc. by reading the installed
`SKILL.md`.

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

## Installable skill packs

A **skill pack** is a directory of markdown the developer installs into
their own project at a tool-specific path. Coding agents that support a
native "skills" mechanism (Claude Code, Codex, Gemini CLI, GitHub Copilot,
Opencode) auto-load anything under their conventional skills directory,
which means once a pack is installed the developer's agent reads it on
every turn without the user prompting.

Skill packs are **different from `azd ai doc` topics**:

| | `azd ai doc <category> <topic>` topics | Installable skill packs |
|---|---|---|
| Lives | Embedded in `azure.ai.docs` binary | Copied into the developer's project repo |
| Read by | An agent that runs `azd ai doc ...` on demand | An agent that auto-loads its skills directory on every turn |
| Activation | Explicit (agent runs the command) | Implicit (the agent's native skill mechanism) |
| Update path | Re-install the docs extension | Re-run `azd ai doc skills install --force` |
| Content style | Per-task reference (5-8 KB per topic) | Single condensed end-to-end walkthrough |

Both surfaces coexist. Skill packs are how a developer delegates the
whole setup; topics are how an agent fetches narrow reference docs while
working.

### Command surface

```
azd ai doc skills                           # `skills` subcommand parent (lists verbs)
azd ai doc skills install --target <tool> [--path <dir>] [--force] [--no-prompt] [--output text|json]
```

Built-in targets (the shared `.agents/skills/<pack>/` path is the
emerging cross-tool convention; Claude Code uses its own dotfolder):

| `--target` | Install path (relative to cwd) |
|---|---|
| `claude` | `.claude/skills/azd-ai-skill/` |
| `codex` | `.agents/skills/azd-ai-skill/` |
| `gemini` | `.agents/skills/azd-ai-skill/` |
| `copilot` | `.agents/skills/azd-ai-skill/` |
| `opencode` | `.agents/skills/azd-ai-skill/` |
| `custom` | User-supplied via `--path` |

JSON success shape (`--output json`):

```json
{
  "status": "installed",
  "target": "copilot",
  "path": ".agents/skills/azd-ai-skill",
  "files": ["SKILL.md"]
}
```

### Filesystem layout

```
cli/azd/extensions/azure.ai.docs/internal/cmd/
  skill_install.go                # SkillInstallAction + RunE dispatch
  skill_install_test.go
  skills.go                       # `skills` subcommand parent
  skills/
    SKILL.md                      # the bundled skill installed by `skills install`
    ... (optional supporting files at this root)
    <category>/                   # read-only topic docs (separate surface)
      <topic>.md
```

`//go:embed skills/SKILL.md` in `skill_install.go` pulls the bundled
skill in at build time. Add another shipped file by extending the embed
directive (e.g. `//go:embed skills/SKILL.md skills/helpers`); the
read-only topic subdirectories under `skills/<category>/` are owned by a
separate `embed.FS` in `doc_agent.go` and are not part of the install
surface.

### Safety contract

The install is **file-ownership safe** -- only files that ship in the
embedded pack are ever touched on disk. Foreign files in the destination
directory (user edits, files from another skill, the developer's notes)
are never modified or removed.

Concrete rules implemented by `SkillInstallAction.Run`:

- Without `--force`: refuse the install when any owned file already
  exists at the destination with content that differs from the bundled
  version. Idempotent when content matches byte-for-byte.
- With `--force`: overwrite only files in the pack manifest. Leave
  unknown files untouched.
- Refuse when an owned-file path is occupied by a directory or symlink;
  surface a clear "remove it manually" hint.
- Atomic writes (write to `.tmp` + rename) so a half-write does not
  leave the destination in a torn state.

### Custom-path validation

`--target custom` requires `--path <dir>`, and `<dir>` is validated
before any disk access:

- Empty, `.`, `..` rejected.
- Absolute paths (POSIX or Windows drive-qualified) rejected.
- Leading `/` or `\` rejected (catches forward-slash absolute paths on
  every OS).
- `filepath.Abs(filepath.Join(cwd, path))` must resolve under cwd
  (defeats `../escape`).
- If any existing parent is a symlink, `EvalSymlinks` must still resolve
  inside cwd (defeats symlink escape).

The same containment check is applied per-file during extraction, as
defense in depth against future pack edits.

### Action-object pattern

`SkillInstallAction` follows the action-object pattern enforced across
the `azure.ai.*` extensions: cobra `RunE` validates flags, constructs
the action, and calls `action.Run(ctx)`. No business logic lives in
RunE. See the existing `SampleListAction` / `ShowAction` / etc. in
`azure.ai.agents/internal/cmd/` for the same pattern, and trangevi's
review comment on PR #8266 (commit `discussion_r3276590928`) for the
rationale.

## Agent-driven onboarding pre-flow

A sibling extension's `init` command can opt in to a **pre-flow** that
asks the developer whether to delegate the whole setup to their coding
agent. The pre-flow installs the right skill pack, copies a tailored
starter prompt to the clipboard, and exits without running the existing
interactive init.

Reference implementation: `azure.ai.agents/internal/cmd/init_preflow.go`
+ `init.go` integration + `starter_prompts/agent_init.md`.

### The three-question pattern

The pre-flow runs only when `!flags.noPrompt`. The shape is intentionally
the same for every sibling that adopts it so users build muscle memory:

```
Q1 [Confirm]  "Do you want your coding agent to set up and create an
              <thing> in <product>?"
              default No (existing flow is the path of least surprise)
                -> No  : return (handled=false) -> existing init runs
                -> Yes : continue

Q2 [Confirm]  "Install the <pack-name> skill for your coding agent?"
              default Yes
                -> Yes : Q3
                -> No  : skip to starter prompt

Q3 [Select]   "Which coding agent are you using?"
              6 choices: claude / codex / gemini / copilot / opencode / custom
              custom -> [Prompt] "Custom install path (relative to cwd):"

Install      Shell out: `azd ai doc skills install --target <X> --no-prompt --output json`
             (or `--path <Y>` for custom). Parse the JSON receipt for the
             final install path. On extension-missing: pre-check via
             `azd ext list -o json` and offer to install azure.ai.docs on
             explicit consent (see "Why pre-check" below).

Starter      Render the embedded starter-prompt template (text/template
prompt       syntax with ProjectPath + SkillPath vars), print it verbatim
             with a bold-yellow header, then offer clipboard copy.

Clipboard    Pre-detect headless environments (CI=true, TERM=dumb, SSH
             session, Linux with no DISPLAY/WAYLAND_DISPLAY) and SKIP
             the Copy prompt entirely in those cases. Otherwise prompt
             and soft-fail on library errors.

Ready-to-go  Bold yellow "You're ready to go!" header + tool-specific
             paste instruction ("Open GitHub Copilot Chat and paste the
             prompt.") + manual-fallback commands + docs link.

Return       (handled=true) -- caller MUST skip the existing init flow.
```

### Run signature

The action's `Run(ctx)` returns `(handled bool, err error)`. The cobra
RunE pattern:

```go
if !flags.noPrompt {
    preflow := &InitPreflowAction{
        out:       cmd.OutOrStdout(),
        azdClient: azdClient,
        runner:    defaultAzdRunner,
        cwd:       cwd,
        copyClip:  CopyToClipboard,
    }
    handled, err := preflow.Run(ctx)
    if err != nil {
        return err
    }
    if handled {
        return nil // skip the existing InitAction
    }
}
// existing init flow runs unchanged
```

`handled == false` means the user declined Q1; the caller proceeds with
the existing interactive init. `handled == true` means the pre-flow
already showed the user what to do next and the caller MUST NOT also
show its own UX. The boolean is a contract -- never re-enter the
existing flow when `handled == true`.

### Starter prompts live in the sibling extension

Per-extension ownership pattern: the starter-prompt template lives in
the extension that owns the workflow, NOT in `azure.ai.docs`. The agents
extension keeps:

```
azure.ai.agents/internal/cmd/
  starter_prompts/
    agent_init.md          # text/template with .ProjectPath, .SkillPath
  starter_prompt.go        # renderStarterPrompt + CopyToClipboard helpers
```

Other ai.* extensions adopting the pre-flow drop their own templates
under their own `starter_prompts/` dir. Rationale:

- Starter prompts are tightly coupled to the sibling extension's actual
  workflow; coupling them to `azure.ai.docs`'s release cadence would
  slow iteration.
- Templates are typically small (~1 KB) so duplication is cheap.
- Per-extension ownership keeps the cross-extension contract narrow:
  `azure.ai.docs` owns *installable* content; each sibling owns its
  *invocation-time* content.

### Why pre-check instead of relying on azd auto-install

`azd` ships an auto-install path (`cli/azd/cmd/auto_install.go`) that
detects when a command belongs to an uninstalled extension and offers
to install it. With `--no-prompt` the auto-install proceeds silently
because `console.Confirm` returns the prompt's `DefaultValue` (`true`).

**It does not work for our case.** The pre-parser
(`extractFlagsWithValues`) only knows flags declared on the current
command tree. Extension-specific flags like `--target` and `--path` do
not exist until `azure.ai.docs` is installed, so the pre-parser treats
`copilot` (a `--target` value) and `json` (an `--output` value) as
positional args, mis-detects the command, and the re-run fails with
`unknown flag: --target` even though the extension was just installed
successfully.

The pre-flow therefore pre-checks via `lookupExtension` (parses
`azd ext list -o json`) and explicitly dispatches
`azd ext install azure.ai.docs` on user consent before ever attempting
the skill install. See the long-form comment at the top of
`azure.ai.agents/internal/cmd/ext_lookup.go` and commit `f6c38d28b`
for the empirical reproduction.

### Banner suppression

The decorative ASCII banner that `azd ai agent init` prints is
suppressed in two cases:

- `--no-prompt` mode (CI/automated runs) -- banner noise in
  machine-parsed logs.
- (Future) when Q1=Yes -- the user is delegating to their coding agent
  and never sees the banner anyway.

The current `init.go` suppresses on `--no-prompt`; the Q1=Yes
suppression is a small follow-up that was deferred so the pre-flow's
own output style stays consistent during initial rollout.

### Targets table is shared but duplicated

Both `azure.ai.docs/internal/cmd/skill_install.go` and
`azure.ai.agents/internal/cmd/init_preflow.go` maintain their own
copy of the target table:

- The docs-side table drives `--target` validation and the install
  path lookup.
- The agents-side table drives Q3's Select choice list, the
  gray-colored path labels, and the per-tool paste instructions in
  the ready-to-go block.

The duplication is intentional -- the agents extension needs richer
metadata (`pasteInstruction`, gray-formatted label) that the docs
extension does not. Tests in each extension pin its own table; the
drift guard is `TestPreflowTargets_PathsAlignWithDocsExtension` in
the agents extension, which fails loudly if the path arrangement
diverges.

**Reverse-lookup hazard**: codex / gemini / copilot / opencode all
install to the same path (`.agents/skills/azd-ai-skill/`). Code that
reverse-resolves the chosen target by `installPath` would always
match the first entry (codex) regardless of what the user picked. The
pre-flow MUST track the chosen target directly from Q3 -- see
`TestPrintReadyToGo_UsesPasteInstructionFromChosenTarget` (commit
`a2637a8be` fixed an instance of this bug).

## Adding the onboarding pre-flow to a new sibling extension

Three mechanical steps once the sibling already has an interactive init
command and the docs extension already ships a relevant skill pack:

1. **Author the starter-prompt template.** Drop
   `internal/cmd/starter_prompts/<name>.md` into the sibling extension.
   Use `text/template` syntax for any per-project substitutions
   (`ProjectPath`, `SkillPath`, etc.). Keep it under ~2 KB.
2. **Add the per-extension targets table + action.** Copy
   `init_preflow.go` from `azure.ai.agents` as a starting point.
   Adjust:
   - `preflowTargets` (mostly the same; just verify paths match the
     skill pack's install paths in `azure.ai.docs`).
   - `askDelegate` Q1 message ("set up a `<thing>` in `<product>`?").
   - `askInstallSkill` Q2 message (skill pack display name).
   - The dispatch args in `installSkill` (`--target X`).
   - The ready-to-go block's "what your agent will do" paragraph.
3. **Wire the dispatch in the sibling's `init` RunE.** After flag
   resolution + `azdClient` construction, before the existing init
   logic:
   ```go
   if !flags.noPrompt {
       preflow := &InitPreflowAction{...}
       handled, err := preflow.Run(ctx)
       if err != nil { return err }
       if handled { return nil }
   }
   ```

That is the entire onboarding. No coordination with `azure.ai.docs` is
required beyond confirming the skill pack name + install paths match.

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
- **No reliance on `azd` auto-install for the skill install dispatch.**
  Auto-install does run when shelling out programmatically, but mis-
  parses extension-specific flag values (`--target`, `--path`) and the
  re-run after install fails with `unknown flag: --target`. The pre-flow
  pre-checks via `azd ext list -o json` and explicitly installs
  `azure.ai.docs` on user consent. See `ext_lookup.go` for the long-form
  comment and commit `f6c38d28b` for the empirical reproduction.
- **No `azd ai doc skills list / uninstall / update`.** Reserved for
  future verbs under the same subtree but explicitly not in scope for
  the initial rollout. Re-running `install --force` covers update; an
  uninstall would need to track an owned-files manifest on disk, which
  we deliberately did not ship to keep `--force` safe.
- **No `azd ai agent init --install-skill <target>` flag.** The
  non-interactive equivalent is `azd ai doc skills install --target X
  --no-prompt`. Add the init flag only if automation users actually
  ask for one-shot init+install.
- **No auto-install of `azure.ai.docs` from inside the pre-flow without
  user consent.** Detected absence triggers an explicit Confirm prompt.
- **Starter prompts do NOT live in `azure.ai.docs`.** They are
  per-extension content (see "Starter prompts live in the sibling
  extension" above). This is deliberate; do not move them.

## References

- Reference implementation extension: `cli/azd/extensions/azure.ai.docs/`
- First onboarded sibling: `cli/azd/extensions/azure.ai.agents/`
- Confirmation envelope contract: agents `README.md` (Confirmation
  envelope for write commands section)
- Action-object pattern rationale: trangevi review on PR #8266
  (`https://github.com/Azure/azure-dev/pull/8266#discussion_r3276590928`)
- Implementation commits on `agent-ready` branch:
  - `2926d5d23` -- initial `azure.ai.docs` extension scaffolding
  - `7b60c685b` -- moved topic content from agents into the docs extension
  - `b28ae56fd` -- reserved-flag fix (use SDK `RegisterFlagOptions`)
  - `0841bd3a5` -- `azd ai doc skills install` command + `azd-ai-skill` pack
  - `99f46dc22` -- embedded starter prompt + clipboard helper (atotto dep)
  - `1021a3f05` -- cross-extension lookup + dispatch helpers
  - `a29867cae` -- agent-driven onboarding pre-flow on `azd ai agent init`
  - `f6c38d28b` -- documented why we pre-check the docs extension
  - `a2637a8be` -- fixed reverse-lookup bug; track chosen target directly
  - `4d2b0b00f` -- renamed Microsoft Foundry skill to AZD AI skill
