---
short: Edit the on-disk agent.yaml (env vars, endpoint, card, codeConfiguration, container resources).
order: 25
---
# Extend: edit the on-disk `agent.yaml`

Audience: an AI coding assistant changing the on-disk agent definition -- runtime env vars, endpoint contract, agent card, container/code-deploy settings, image reference. This topic covers ONLY the file at `<service-dir>/agent.yaml`. Service-level config (model deployments, connections, toolboxes, tool resources) lives in `azure.yaml` -- see `configure`. Connection-specific guidance (auth types, credential shapes, MCP recipes) lives in `azd ai doc connection <topic>`.

---

## Two files, two schemas (read this first)

After `azd ai agent init -m <manifest-url>`, there are TWO files that together define the agent. They have DIFFERENT schemas and they get loaded by DIFFERENT code paths at deploy time. The single most common reason a hand-edited project fails to deploy is putting the right field in the wrong file.

| File                                | Schema                                | What lives here                                                                                                              |
| ----------------------------------- | ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `<service-dir>/agent.yaml`          | Flat `ContainerAgent` (this topic)    | `kind`, `name`, `protocols`, `environment_variables`, `agentEndpoint`, `agentCard`, `codeConfiguration`, `image`, container `resources` (cpu / memory). |
| `azure.yaml` `services.<name>.config` | `ServiceTargetAgentConfig` (`configure`) | `deployments[]` (model deployments), `connections[]`, `toolConnections[]`, `toolboxes[]`, `resources[]` (built-in tool resources w/ connection names), `container`, `startupCommand`. |
| `<env>/.env` (via `azd env set`)     | Flat KV                               | Per-environment secrets and `PARAM_<CONN>_<KEY>` credential values that the `azure.yaml` connection blocks reference as `${PARAM_...}`. |

`agent.manifest.yaml` (the file format passed to `azd ai agent init -m`) is NOT on disk after init. It is the SEED format that has a `template:` wrapper, an outer `parameters:` block, and an outer `resources[]` array. Init splits it across the three files above and discards the wrapper. Do NOT bring the `template:` wrapper back into the on-disk `agent.yaml` -- the deploy parser reads the file as a flat ContainerAgent (`yaml.Unmarshal(data, &agentDef)`), and a `template:` key at the root is silently ignored, which causes deploys to use defaults instead of your overrides.

If you're holding a manifest fragment from a sample and want to apply it post-init, treat it as a recipe: extract the relevant pieces into the right files. Section "What lives where" below maps every common edit to its target file.

---

## What lives where (quick lookup)

| You want to ...                                       | Edit ...                                                       | Topic            |
| ----------------------------------------------------- | -------------------------------------------------------------- | ---------------- |
| Add or change an env var the container reads          | `agent.yaml` `environment_variables[]`                         | this topic       |
| Change the endpoint protocols served / version mix    | `agent.yaml` `agentEndpoint:`                                  | this topic       |
| Edit the A2A agent card                               | `agent.yaml` `agentCard:`                                      | this topic       |
| Switch container vs. code deploy / pick a runtime     | `agent.yaml` `codeConfiguration:` / remove for container mode  | this topic       |
| Change CPU / memory                                   | `agent.yaml` `resources:` (cpu, memory)                        | this topic       |
| Swap a model deployment                               | `azure.yaml` `services.<name>.config.deployments[]`            | `configure`      |
| Add / remove a connection                             | `azure.yaml` `services.<name>.config.connections[]`            | `connection add` |
| Add / remove a toolbox or a tool inside a toolbox     | `azure.yaml` `services.<name>.config.toolboxes[]` (+ `toolConnections[]` for external MCP/OpenAPI/A2A tools) | `configure` + `connection add` |
| Wire a built-in tool that needs a named connection (bing_grounding, azure_ai_search) | `azure.yaml` `services.<name>.config.resources[]` | `configure`      |
| Set a credential value referenced as `${PARAM_...}`   | `azd env set PARAM_<CONN>_<KEY> <value>`                       | `connection auth-types` |
| Patch endpoint / card without a full redeploy         | `agent.yaml` (this topic), then `azd ai agent endpoint update` | `configure`      |

If the change is in `agent.yaml`, you need a full `azd deploy` for it to take effect (which creates a new immutable agent version). If the change is in `azure.yaml`'s `config.connections[]` or `config.deployments[]`, you typically need `azd provision` first to let Bicep create / update the resource, then `azd deploy`.

---

## `kind:` -- which agents this extension can deploy

`azd ai agent` validates and deploys exactly two kinds. The full AgentSchema spec includes more, but anything outside this list fails validation in `azd ai agent doctor` and `azd deploy`:

| `kind:`     | When to use                                                          |
| ----------- | -------------------------------------------------------------------- |
| `hosted`    | Container-backed agent (Python / .NET / Node) running on Foundry.    |
| `workflow`  | Multi-step orchestration with a declarative `trigger:`. Preview.     |

A `kind: prompt` block from raw AgentSchema docs does NOT deploy through this extension -- the parser rejects it. Use `hosted` for prompt-driven agents and put the system prompt in the agent's source code or environment.

---

## Hosted agent (`kind: hosted`)

The on-disk shape. Maps directly to the `ContainerAgent` Go type. Every field is optional except `kind` and `name`.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/microsoft/AgentSchema/refs/heads/main/schemas/v1.0/ContainerAgent.yaml
kind: hosted
name: my-agent
description: Answers questions about the docs corpus.
protocols:
  - protocol: responses
    version: "1.0.0"
  - protocol: invocations
    version: "1.0.0"
resources:
  cpu: "0.25"
  memory: "0.5Gi"
environment_variables:
  - name: AZURE_AI_MODEL_DEPLOYMENT_NAME
    value: ${AZURE_AI_MODEL_DEPLOYMENT_NAME}
  - name: LOG_LEVEL
    value: info
codeConfiguration:
  runtime: python_3_13
  entryPoint: app.py
  dependencyResolution: remote_build   # or "bundled"
agentEndpoint:
  protocols: ["responses"]
  versionSelector:
    versionSelectionRules:
      - type: traffic
        agentVersion: "3"
        trafficPercentage: 100
  authorizationSchemes:
    - type: entra
agentCard:
  description: "What this agent does for users."
  skills:
    - id: answer-docs
      name: Answer documentation questions
      description: Cite a docs URL with each answer.
      examples:
        - "How do I rotate the API key?"
```

Key blocks:

* `protocols:` -- wire formats this agent serves. `responses` is the OpenAI Responses API shape; `invocations` is the A2A invocations shape. Most agents advertise both. Editing this requires a full redeploy.
* `resources:` -- container CPU / memory (NOT to be confused with the manifest's outer `resources[]` array, which is a different concept and doesn't exist in this file). Valid values mirror what `azd ai agent init` offered: `0.25 / 0.5Gi`, `1 / 2Gi`, `2 / 4Gi`.
* `environment_variables:` -- per-version env vars injected at runtime. Two reference forms:
  * `${VAR}` -- resolved from the active azd environment at deploy time (use this for env-specific values like endpoints or model deployment names).
  * `{{VAR}}` -- resolved at `azd ai agent init` time from the manifest's `parameters:` block. AFTER init the placeholder is replaced with the literal value; don't reintroduce `{{...}}` to an on-disk `agent.yaml`. (The BYO-toolbox sample's post-init checklist explicitly calls this out: replace any remaining `${{VAR}}` with `${VAR}`.)
  * Env vars are NOT for secrets. Use a connection in `azure.yaml` and let the platform inject the credential.
* `codeConfiguration:` -- present only when `azd deploy` should ZIP-upload your source instead of building / pushing a container image. Required fields: `runtime` (`python_3_13`, `python_3_14`, `dotnet_10`, `node_22`) and `entryPoint` (`app.py`, `MyAgent.dll`, `dist/index.js`). Optional `dependencyResolution` is `remote_build` (default; Foundry resolves) or `bundled` (your build packs everything). Absence of this block means container deploy (`azure.yaml`'s service must have a `docker:` section).
* `image:` -- pre-built container image reference (e.g. `myregistry.azurecr.io/myagent:v1`). When set, deploy can skip the Dockerfile build. Interactive mode prompts; `--no-prompt` defaults to building from the Dockerfile.
* `agentEndpoint:` -- selects which deployed versions traffic routes to AND which protocols the endpoint serves. Editing THIS block in isolation does NOT require a full redeploy -- use `azd ai agent endpoint update` (see `configure`) for an in-place patch.
* `agentCard:` -- the A2A "agent card" that advertises capabilities to other agents. Edited the same way as `agentEndpoint:` (in-place patch via `endpoint update`).

---

## Workflow agent (`kind: workflow`) -- preview

```yaml
kind: workflow
name: nightly-report
trigger:
  schedule:
    cron: "0 3 * * *"
```

Workflow agents are declarative orchestrations. The `trigger:` block is free-form -- consult the AgentSchema docs for the trigger types currently accepted by the Foundry runtime. There is no separate CLI verb for triggers; the schedule lives in the manifest.

---

## What this topic does NOT cover

* `azure.yaml` `services.<name>.config.*` -- model deployments, connections, toolboxes, tool resources, startup command. See `configure`.
* Adding or editing connections (declarative or imperative) -- see `azd ai doc connection overview` first; `azd ai doc connection add` for recipes.
* Picking a model deployment / Foundry project at init time -- see `initialize`.
* Deploying changes after edit -- see `deploy`.
* The `agent.manifest.yaml` format -- not on disk after init; see `initialize` for the seed-time contract.

---

## Round-trip safety

When you edit on-disk `agent.yaml`:

* The parser preserves unknown fields, but `azd ai agent doctor` flags them. Treat any "unknown field" as the strongest signal that the field name is mistyped or that you're pasting in something from the `agent.manifest.yaml` format (which has fields this file doesn't).
* Validate locally before deploy:

```bash
azd ai agent doctor --output json
```

Look for the `agent-yaml-valid` check; the failure message names the field path that failed validation.

* Re-deploys after editing the file create a NEW immutable agent version. Old versions remain on the Foundry project until removed. Use `agentEndpoint.versionSelector` to control which version receives traffic.
