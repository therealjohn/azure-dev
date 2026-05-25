---
name: azd-ai-skill
description: Set up, scaffold, configure, deploy, evaluate, and operate AI agents on Microsoft Foundry using the Azure Developer CLI (azd) and the azure.ai.agents extension. USE FOR azd ai agent, foundry agent, agent.yaml, azure.yaml service config, hosted agent, deploying agents to Azure, running an agent locally, evaluating an agent, optimizing an agent, adding a tool to an agent, web search, code interpreter, file search, function tool, MCP server, OpenAPI tool, A2A peer agent, Azure AI Search RAG, Bing grounding, Bing Custom Search, toolbox, toolbox version, connection, RemoteTool, CognitiveSearch, OAuth2, UserEntraToken, AgenticIdentity, ProjectManagedIdentity, ApiKey, CustomKeys, model deployment, Foundry project endpoint. DO NOT USE FOR generic Azure CLI tasks unrelated to Foundry, or LLM application code that does not deploy to a Foundry hosted agent.
allowed-tools: ["azd", "azd ai agent", "azd ai doc", "azd version", "azd extension list", "azd auth login", "azd config get defaults", "azd env get-values"]
---
# AZD AI skill

You're driving `azd` and the `azure.ai.agents` extension on behalf of a developer. This file is the router. Pull a topic on demand for the details.

## Defaults

* Add `--output json` and `--no-prompt` to `azd ai agent ...` commands so output is scriptable. **Do not** add `--output json` to `azd ai doc ...` -- doc commands print markdown either way. Read the topic body once; don't `grep` through it.
* Prefer `azd` over `az`. `azd` already knows the project, subscription, and Foundry endpoint. Only fall back to `az` after `project show`, `config get defaults`, and `env get-values` come up empty AND the developer has been asked.
* Stop and ask the developer when a topic says "ask the developer" or when a write command exits 2 with a `confirmation_required` envelope.
* **Never** run `azd auth login` yourself. It opens a browser. Ask the developer.

## Start every session with

```bash
azd version --output json
azd extension list --output json     # must include azure.ai.agents
azd auth login --check-status
azd ai agent project show --output json
azd ai agent show --output json
```

Branch on `show`'s `.status`:

* `active` / `deployed` -> jump to `investigate` (diagnose) or `operate` (change remote state).
* `not_deployed` with `next_step.suggestions[]` -> run the suggested command. For a greenfield init, always start with `azd ai agent sample list --output json` to pick a `manifestUrl`, then `azd ai agent init -m <manifestUrl>`. Use `--from-code` only when the cwd already has hand-written agent source.
* Anything else -> `azd ai agent doctor --output json` and surface failing checks.

## Topics: agent workflow

```bash
azd ai doc agent <topic>
```

| Want to ...                                                  | Topic         |
| ------------------------------------------------------------ | ------------- |
| Pick a starting sample (any greenfield init)                 | `samples`     |
| Bootstrap a new agent project (`azd ai agent init`)          | `initialize`  |
| Run + iterate locally (`azd ai agent run`)                   | `develop`     |
| Edit `azure.yaml` service config (models, toolboxes, env)    | `configure`   |
| Edit on-disk `agent.yaml` (env vars, endpoint, card, runtime)| `extend`      |
| Provision, deploy, version, `.agentignore`                   | `deploy`      |
| Generate, run, iterate evals                                 | `evaluate`    |
| Invoke (billed), files, sessions, optimize, endpoint patches | `operate`     |
| Inspect state, sessions, logs, files, doctor                 | `investigate` |

List all: `azd ai doc agent`.

## Topics: connections

For everything connection-related (MCP, Azure AI Search, Bing, OpenAPI, A2A; auth types; credentials):

```bash
azd ai doc connection <topic>
```

| Want to ...                                                  | Topic         |
| ------------------------------------------------------------ | ------------- |
| Mental model (declarative vs. pre-existing vs. imperative)   | `overview`    |
| Step-by-step recipes for common scenarios                    | `add`         |
| `category:` reference                                        | `categories`  |
| `authType:` + credentials + `PARAM_*` env-var rule           | `auth-types`  |
| Imperative CLI (`connection list / show / create / ...`)    | `manage`      |

## Topics: toolboxes

For grouping multiple tools under one MCP endpoint (`mcp`, `web_search`, `code_interpreter`, `azure_ai_search`, `openapi`, etc.):

```bash
azd ai doc toolbox <topic>
```

| Want to ...                                                  | Topic         |
| ------------------------------------------------------------ | ------------- |
| Mental model + how azd deploys / versions toolboxes          | `overview`    |
| Step-by-step recipes (web search, MCP + connection, mixed)   | `add`         |
| Reference of tool types you can put in a toolbox             | `tools`       |
| Agent-side runtime wiring (env var, MCP client, header)      | `consume`     |

## Resolving subscription, location, project ID

For **subscription** or **location**, try in order:

1. `azd ai agent project show --output json`
2. `azd config get defaults`
3. `azd env get-values`
4. Ask the developer.
5. Last resort, with explicit consent: `az account list --output json`.

For the **Foundry project ARM ID** (`--project-id`), there's no safe `az` fallback. Try `azd ai agent project show --output json`; otherwise ask the developer and include this hint:

> Open https://ai.azure.com -> Operate -> Admin -> select your project -> Copy the Resource ID.

Don't shell out to `az cognitiveservices` or `az resource list` for the project ID -- they return the wrong resource shape.

## Confirmation envelope (exit 2)

Destructive or billed commands print JSON like this and exit 2 when run with `--no-prompt` and no `--force`:

```json
{ "status": "confirmation_required", "command": "...", "changes": [...], "confirmCommand": "... --force" }
```

Rules:

* Summarize `changes[]` for the developer in plain English.
* If their **immediately prior** turn named this exact action ("deploy", "yes delete it"), they've already consented -- re-run with `--force`.
* Otherwise, get explicit consent first. Never auto-append `--force`.
* Run `confirmCommand` exactly as printed.

For the full envelope shape, see `azd ai doc agent operate`.

## When to stop and ask

* `--project-id` when not provided. Ask first; share the portal hint above.
* Picking a model deployment when multiple are available.
* Any `confirmation_required` envelope (unless prior turn already named it).
* Any nonzero exit from `auth login --check-status`, `provision`, or `deploy` that lacks a `next_step` block.
* Anything the developer flagged "ask first".
