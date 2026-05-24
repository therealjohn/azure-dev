---
name: azd-ai-skill
description: Scaffold, provision, deploy, evaluate, and operate AI agents on Microsoft Foundry from the terminal using the Azure Developer CLI (azd) and the azure.ai.agents extension. USE FOR azd ai agent, foundry agent, agent.yaml, deploying agents to Azure, running an agent locally, evaluating an agent, optimizing an agent. DO NOT USE FOR generic Azure CLI tasks, non-Foundry agent runtimes (LangChain, Autogen, Semantic Kernel), or LLM application code that does not target Foundry.
---
# AZD AI skill

Audience: an AI coding assistant driving `azd` and the
`azure.ai.agents` extension on behalf of a developer. This file is the
ROUTER -- the deeper, per-job documentation lives in topic files you
can pull on demand.

Defaults this skill assumes:

* `--output json` and `--no-prompt` so every command is scriptable.
* Stop and ask the human when this file (or a topic) says "ask the
  human" OR when a write command returns a `confirmation_required`
  envelope (exit code 2).
* Prefer `azd` over `az` (the Azure CLI). `azd` already knows the
  user's project context, subscription, and Foundry endpoint. Only
  shell out to `az` after `azd` (project show, config get defaults,
  env get-values) has been tried AND the human has been asked.

----------------------------------------------------------------------

## Topic chooser

For deeper guidance, pull ONE topic via:

```bash
azd ai doc agent <topic>
```

| Want to ...                                                    | Topic        |
| -------------------------------------------------------------- | ------------ |
| Pick a starting sample (DO THIS for any greenfield init)       | `samples`    |
| Bootstrap a brand-new agent project (`azd ai agent init`)      | `initialize` |
| Run + iterate on the agent LOCALLY (`azd ai agent run`)        | `develop`    |
| Shape the agent operationally (connections, files, env vars)   | `configure`  |
| Edit `agent.yaml` (model, tools, protocols, endpoint, card)    | `extend`     |
| Provision + deploy + multi-service + versions + `.agentignore` | `deploy`     |
| Generate / run / iterate on evals                              | `evaluate`   |
| Invoke (billed), files mut, sessions mut, optimize, endpoint   | `operate`    |
| Read-only inspection (state, sessions, logs, files, doctor)    | `investigate`|

List all topics: `azd ai doc agent`.

----------------------------------------------------------------------

## Always do this first

```bash
azd version --output json
azd extension list --output json    # must include azure.ai.agents
azd auth login --check-status
azd ai agent project show --output json
azd ai agent show --output json
```

`project show` returns identity + subscription + Foundry project
endpoint. `show` returns the deployed-agent state. Branch on
`show`'s `.status`:

* `"active"` / `"deployed"` -- a deployed agent already exists. Jump
  to `investigate` (if diagnosing) or `operate` (if changing
  remote state).
* `"not_deployed"` with a `next_step.suggestions[]` -- run the
  suggested command. Usually `azd deploy` (existing scaffold) or
  `azd ai agent init` (no scaffold yet). Before running
  `azd ai agent init` for a greenfield project, ALWAYS call
  `azd ai agent sample list --output json` first to pick a
  `manifestUrl`; then invoke init with `-m <manifestUrl>`. Pull the
  `samples` topic for the catalog contract. Reserve `--from-code`
  for brownfield (the cwd already contains hand-written agent
  source).
* Other status or nonzero exit -- run `azd ai agent doctor
  --output json` and surface failing checks to the human.

Do NOT run `azd auth login` yourself -- it requires a browser. Ask
the human.

## Resolving subscription and location

For SUBSCRIPTION or LOCATION (NOT project ID -- see the next section
for that), use this cascade in order; stop at the first level that
answers your question. Do NOT skip ahead to `az` -- it confuses users
who picked azd specifically to avoid juggling two CLIs.

1. `azd ai agent project show --output json` -- subscription,
   tenant, location, Foundry endpoint already wired into the
   active project.
2. `azd config get defaults` -- user-level azd defaults
   (subscription, location). Returns JSON:
   `{ "location": "...", "subscription": "..." }`.
3. `azd env get-values` -- the active azd environment's variables.
4. Ask the human.
5. Last resort, and ONLY for subscription/location: `az account list
   --output json`. Use this ONLY when 1-4 have been exhausted AND
   the human has approved the shell-out. This cascade does NOT
   apply to the project ID -- see the next section.

----------------------------------------------------------------------

## Resolving the Foundry project ARM ID

The `--project-id` flag (or any other place that needs a Foundry
project ARM ID, like `/subscriptions/.../providers/Microsoft.AI/projects/<name>`)
is DIFFERENT from subscription/location. There is no safe `az` fallback:
`az cognitiveservices ...` and `az resource list ...` will return the
wrong shape of resource, or the wrong project entirely, and silently
target the wrong agent.

Resolution order, with NO discovery shortcuts:

1. `azd ai agent project show --output json` -- if the active azd env
   already has `AZURE_AI_PROJECT_ENDPOINT` wired, the response includes
   a Foundry project record you can map to an ARM ID.
2. Otherwise ASK THE HUMAN. When you ask, include these discovery
   instructions verbatim so the human knows where to look:

   > Open the Foundry portal at https://ai.azure.com -> Operate ->
   > Admin -> select your project -> Copy the Resource ID.

Do NOT shell out to `az cognitiveservices ...`, `az resource list ...`,
or any other `az` command to discover the project ARM ID. The cascade
in the previous section is scoped to subscription/location ONLY.

----------------------------------------------------------------------

## Confirmation envelope (exit code 2)

Destructive / billed commands emit this JSON on stdout and exit 2
when invoked with `--no-prompt` but without `--force`:

```json
{
  "status": "confirmation_required",
  "command": "agent files delete",
  "description": "Delete X from agent Y.",
  "classification": { "destructive": true, "idempotent": false },
  "changes": ["Will delete file X from session Y of agent Z"],
  "confirmCommand": "azd ai agent files delete X --force"
}
```

Rules:

* Summarize `changes[]` for the human in plain English.
* If the human's IMMEDIATELY PRIOR turn explicitly named this action
  (e.g. they said "deploy" or "yes deploy now" and the envelope is for
  `azd deploy`), they have ALREADY consented -- re-run with `--force`
  without asking again. Re-asking when the human just told you to do
  the exact thing is friction and erodes trust.
* Otherwise, get explicit consent before re-running with `--force`.
* Never auto-append `--force` to a command the human did not name.
* Run `confirmCommand` exactly as printed.

----------------------------------------------------------------------

## Read-only commands you can call freely

```bash
azd ai agent show --output json
azd ai agent project show --output json
azd ai agent doctor --output json
azd ai agent sample list --output json
azd ai agent connection list --output json
azd ai agent files list --output json
azd ai agent sessions list --output json
azd ai agent eval list --output json
azd ai agent optimize list --output json
azd config get defaults
azd env get-values
```

----------------------------------------------------------------------

## When to stop and ask the human

* `--project-id` if the human did not provide one. ASK FIRST -- do NOT
  shell out to `az` to discover it. When you ask, give the human the
  Foundry portal directions from "Resolving the Foundry project ARM ID"
  above so they know where to find it.
* Picking a model deployment when multiple candidates exist.
* Any `confirmation_required` envelope (exit 2) -- unless the human's
  immediately prior turn already named this exact action (see
  "Confirmation envelope" rules above).
* Any nonzero exit from `azd auth login --check-status`,
  `azd provision`, or `azd deploy` that does NOT include a clear
  `next_step` block in the JSON output.
* Anything the human flagged "ask first" in their original request.

