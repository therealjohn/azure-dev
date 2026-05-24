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

----------------------------------------------------------------------

## Topic chooser

For deeper guidance, pull ONE topic via:

```bash
azd ai doc agent <topic>
```

| Want to ...                                                    | Topic        |
| -------------------------------------------------------------- | ------------ |
| Pick a curated starting sample                                 | `samples`    |
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
  `azd ai agent init` (no scaffold yet).
* Other status or nonzero exit -- run `azd ai agent doctor
  --output json` and surface failing checks to the human.

Do NOT run `azd auth login` yourself -- it requires a browser. Ask
the human.

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
* Get explicit consent before re-running with `--force`.
* Never auto-append `--force` -- the human's reply IS the consent.
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
azd env get-values
```

----------------------------------------------------------------------

## When to stop and ask the human

* Picking a `--project-id` if the human did not provide one.
* Picking a model deployment when multiple candidates exist.
* Any `confirmation_required` envelope (exit 2).
* Any nonzero exit from `azd auth login --check-status`,
  `azd provision`, or `azd deploy` that does NOT include a clear
  `next_step` block in the JSON output.
* Anything the human flagged "ask first" in their original request.

