---
name: AZD AI
description: Scaffold, provision, and deploy AI agents to Microsoft Foundry using the Azure Developer CLI (azd) and the azure.ai.agents extension.
---

# AZD AI skill (azd-driven)

Audience: an AI coding assistant driving the `azd ai agent` extension on
behalf of a developer. Every command below is safe to run from a script
unless explicitly noted; this skill prefers `--output json` and
`--no-prompt` so output is machine-parseable and you never block on input.

This skill takes a project from "no agent yet" to "deployed agent on
Microsoft Foundry, responding to invocations". The workflow is linear:

1. Verify the host can run azd and the user is signed in.
2. Resolve identity + project context.
3. Decide whether to scaffold (`azd ai agent init`) or pick up an existing
   service.
4. Configure (`agent.yaml`).
5. Provision Foundry resources (`azd provision`).
6. Deploy the agent (`azd deploy`).
7. Verify with `azd ai agent show` and `azd ai agent invoke`.

Stop and ask the human ONLY when this skill says "ask the human" or when a
command returns a confirmation envelope (exit code 2, see "Confirmation
envelope" below).

----------------------------------------------------------------------

## 0. Preflight (always)

Run these once at the start of the session. If any fail, surface the
error to the human and stop -- do not try to "fix" auth or installation.

```bash
azd version --output json
azd extension list --output json
azd auth login --check-status
```

`azd extension list --output json` MUST include `azure.ai.agents`. If it
does not, run:

```bash
azd extension install azure.ai.agents
```

If `azd auth login --check-status` exits non-zero, ask the human to run
`azd auth login`. Do not run `azd auth login` yourself -- it requires a
browser.

----------------------------------------------------------------------

## 1. Resolve identity and project context

```bash
azd ai agent project show --output json
```

Success payload (all fields optional; trust the keys you see):

```json
{
  "subscription": { "id": "11111111-2222-3333-4444-555555555555" },
  "tenant": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "location": "eastus2",
  "resourceGroup": "rg-myteam",
  "projectEndpoint": "https://contoso.services.ai.azure.com/api/projects/myproj",
  "projectEndpointSource": "azdEnv",
  "foundryAccount": "contoso",
  "projectName": "myproj"
}
```

`projectEndpointSource` is one of `flag`, `azdEnv`, `globalConfig`, or
`foundryEnv`. An empty payload (`{}`) means none of the resolution levels
produced a value -- jump to step 2.

This command never fails when ANY field can be resolved. A nonzero exit
means the azd host itself is unreachable; surface the error to the human.

----------------------------------------------------------------------

## 2. Decide: scaffold a new agent, or continue an existing one

```bash
azd ai agent show --output json
```

Branch on `.status`:

* `"deployed"` -- there is already a Foundry agent. Skip to step 6
  (verify) unless the human asked for a different agent.

* `"not_deployed"` -- there may be a scaffolded service waiting for
  deploy, or no service at all. The payload's `next_step.suggestions[]`
  tells you exactly which command to run next. Follow it.

* Any other status or a nonzero exit -- run `azd ai agent doctor --output
  json` and surface its `checks[]` failures to the human.

If the project has no `azure.yaml` (Foundry project) at the cwd, OR the
only `azure.yaml` is the AZD AI bootstrap stub written by the pre-flow
(`metadata.template: azd-ai-bootstrap@*`), you must scaffold an agent.
The non-interactive scaffold invocation is:

```bash
azd ai agent init --no-prompt \
  --project-id "<projectResourceId>" \
  --deploy-mode code \
  --runtime python_3_13 \
  --entry-point app.py
```

You do NOT need to pass `--from-code` for the post-pre-flow scaffold.
`azd ai agent init` detects the bootstrap stub
(`metadata.template: azd-ai-bootstrap@*`) and routes through the from-code
path automatically.

`--from-code` is reserved for the explicit case where the directory
already contains YOUR AGENT CODE (not just the bootstrap stub). Pass it
when:

* You authored agent source files (Python, .NET, Node.js) in the cwd
  BEFORE running `azd ai agent init`, AND
* You want to be explicit so the command does not attempt to scan the
  directory or fall through to template selection.

When set, `--from-code`:

* Tells `azd ai agent init` to use the code in the current directory as
  the agent's source (instead of downloading a template).
* Is mutually exclusive with `--manifest` -- pick one or the other.

Substitutions:

* `<projectResourceId>` -- the Foundry project ARM ID. Get a candidate
  list with `azd ai agent sample list --output json` (curated catalog).
  If the human already has a Foundry workspace, ask them for the
  resource ID; do not guess.
* `--deploy-mode` -- `code` (your repo's source) or `image` (prebuilt
  container).
* `--runtime` / `--entry-point` -- code-deploy only. Defaults are usable
  for most Python projects (`python_3_13` / `app.py`).

If `azd ai agent init` returns exit code 2 with a confirmation envelope,
inspect `changes[]`, then either re-run with `--force` (after summarizing
the changes to the human and getting consent) or stop and ask.

----------------------------------------------------------------------

## 3. Configure agent.yaml

`azd ai agent init` creates an `agent.yaml`. Before provisioning, verify
the model, skills, and tool list are what the human wants:

```bash
cat agent.yaml
```

Common edits:

* `model:` -- name of the Foundry model deployment to bind to. See
  `azd ai agent connection list --output json` for available
  connections.
* `instructions:` -- the system prompt. Plain text.
* `tools:` -- list of tool integrations (file_search, code_interpreter,
  custom function refs).

Show diff proposals to the human before writing. Do not silently edit.

----------------------------------------------------------------------

## 4. Provision Foundry resources

```bash
azd provision --no-prompt
```

This creates the Foundry project, model deployment, and supporting
resources defined in `infra/`. If the project has `infra/layers/`, layers
provision in parallel.

Failure modes:

* "subscription quota exceeded" -- ask the human to request quota
  increase; do not retry.
* "credential creation failed" -- run `azd auth login --check-status`
  and surface the result.
* Bicep deploy errors -- the message in `error.details[]` tells you what
  failed. Surface verbatim and ask the human.

----------------------------------------------------------------------

## 5. Deploy the agent

```bash
azd deploy --no-prompt
```

For code deploys this zips the source, uploads it, and registers the
agent against the Foundry project. For image deploys it pushes the
container and registers.

When `azd deploy` completes, run step 6 to verify.

----------------------------------------------------------------------

## 6. Verify the deployment

```bash
azd ai agent show --output json
```

Expect `"status": "deployed"` and an `agent` block with the agent's id,
name, model, and endpoint URLs. The endpoint URLs are also written to the
active azd env as `AGENT_<SVC>_<PROTO>_ENDPOINT` (one var per protocol).

Smoke-test an invocation:

```bash
azd ai agent invoke "hello, are you up?" --output json
```

The response includes `messages[]` and a `status`. Anything other than
`"completed"` warrants a follow-up with `azd ai agent doctor --output
json`.

----------------------------------------------------------------------

## 7. Iterate

To make the agent do new things, the loop is:

1. Edit `agent.yaml` (or source code, for code deploys).
2. Re-run `azd deploy --no-prompt`.
3. Re-verify with `azd ai agent show` and `azd ai agent invoke`.

To inspect logs:

```bash
azd ai agent monitor --output json
```

----------------------------------------------------------------------

## Confirmation envelope (exit code 2)

Destructive commands (delete, overwrite, update endpoint, etc.) emit a
confirmation envelope when invoked with `--no-prompt` but without
`--force`. The shape:

```json
{
  "status": "confirmation_required",
  "command": "agent files delete",
  "description": "Delete X from agent Y.",
  "classification": {
    "readOnly": false,
    "destructive": true,
    "idempotent": false
  },
  "changes": ["Will delete file X from session Y of agent Z"],
  "confirmCommand": "azd ai agent files delete X --force"
}
```

Handling:

* Summarize `changes[]` for the human in plain English.
* Get explicit consent before re-running with `--force`.
* Never assume consent. The envelope exists because the command is
  classified destructive.

----------------------------------------------------------------------

## Read-only commands you can call freely

These never mutate state. Use them liberally for diagnostics and
context-gathering:

* `azd ai agent show --output json`
* `azd ai agent project show --output json`
* `azd ai agent doctor --output json`
* `azd ai agent sample list --output json`
* `azd ai agent connection list --output json`
* `azd ai agent files list --output json`
* `azd ai agent sessions list --output json`
* `azd env get-values`

----------------------------------------------------------------------

## When to stop and ask the human

* Picking a `--project-id` if the human hasn't already given you one.
* Picking a model deployment when several options are available.
* Any confirmation envelope (exit code 2).
* Any nonzero exit from `azd auth login --check-status`, `azd provision`,
  or `azd deploy` that does NOT include a clear `next_step` block in the
  JSON output.
* Anything the human flagged as "ask first" in their original instructions.

For deeper reference docs (configure shape, investigate state, operate
contracts), the user can run:

```bash
azd ai doc agent
azd ai doc agent initialize
azd ai doc agent configure
azd ai doc agent investigate
azd ai doc agent operate
```
