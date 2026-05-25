---
short: Edit agent.yaml to shape model, tools, connections, protocols, and endpoint.
order: 25
---
# Extend: edit `agent.yaml` (AgentSchema)

Audience: an AI coding assistant editing the agent manifest to add a tool, swap a model, add a connection, or change the endpoint contract. This topic covers the on-disk shape; commands that READ the file (deploy, endpoint update) live in their own topics.

`agent.yaml` is the source of truth for the deployed agent identity and behavior. `azd deploy` reads it and creates a new immutable agent version on the Foundry project. Round-trip safe -- the parser preserves the manifest layout across edit / deploy / re-deploy.

Full AgentSchema reference: https://microsoft.github.io/AgentSchema/guides/. This topic covers the subset that `azd ai agent` parses and deploys.

---

## File location and basic shape

`agent.yaml` lives at `<service-dir>/agent.yaml`, where `<service-dir>` is the path under `services.<name>.project` in `azure.yaml`. There is one `agent.yaml` per ai.agent service.

The file has TWO top-level layouts:

* **Manifest layout** (canonical) -- a `template:` key wraps the agent definition. Parameters and resources sit at the root. This is what `azd ai agent init` writes.
* **Direct layout** -- the agent definition fields are at the root, no `template:` wrapper. Both forms parse; prefer manifest layout for new files so parameters round-trip cleanly.

Manifest layout skeleton:

```yaml
name: my-agent
displayName: My Agent
description: A helpful agent.
template:
  kind: hosted
  name: my-agent
  # ... agent definition fields below
parameters: {}
resources: []
```

---

## `kind:` -- which agents this extension can deploy

`azd ai agent` validates and deploys exactly two kinds. The full AgentSchema spec includes more, but anything outside this list will fail validation in `azd ai agent doctor` and `azd deploy`:

| `kind:`     | When to use                                                          |
| ----------- | -------------------------------------------------------------------- |
| `hosted`    | Container-backed agent (Python / .NET / Node) running on Foundry.    |
| `workflow`  | Multi-step orchestration with a declarative `trigger:`. Preview.     |

A `kind: prompt` block from raw AgentSchema docs will NOT deploy through this extension -- the parser rejects it. Use `hosted` for prompt-driven agents and put the system prompt in `instructions:` (see below).

---

## Hosted agent (`kind: hosted`)

The most common shape. Maps to `ContainerAgent` in the parser.

```yaml
template:
  kind: hosted
  name: my-agent
  displayName: My Agent
  description: Answers questions about the docs corpus.
  protocols:
    - protocol: responses
      version: "1.0.0"
    - protocol: invocations
      version: "1.0.0"
  codeConfiguration:
    runtime: python_3_13
    entryPoint: app.py
    dependencyResolution: remote_build   # or "bundled"
  environmentVariables:
    - name: LOG_LEVEL
      value: info
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

* `protocols:` -- wire formats this agent serves. `responses` is the OpenAI Responses API shape; `invocations` is the A2A invocations shape. Most agents advertise both.
* `codeConfiguration:` -- present only when `azd deploy` should ZIP- upload your source instead of building a container image. Required fields: `runtime` (e.g. `python_3_13`, `python_3_14`, `dotnet_10`, `node_22`) and `entryPoint` (e.g. `app.py`, `MyAgent.dll`, `dist/index.js`). Optional `dependencyResolution` is `remote_build` (default; Foundry resolves) or `bundled` (your build packs everything).
* `environmentVariables:` -- per-version env vars injected at runtime. These are NOT secrets -- use connections + Key Vault references for secrets.
* `agentEndpoint:` -- selects which deployed versions traffic routes to AND which protocols the endpoint serves. Editing this block in isolation does NOT require a full redeploy -- use `azd ai agent endpoint update` (see `configure`) for an in-place patch.
* `agentCard:` -- the A2A "agent card" that advertises capabilities to other agents. Edited the same way as `agentEndpoint:` (in-place patch via `endpoint update`).

---

## Workflow agent (`kind: workflow`) -- preview

```yaml
template:
  kind: workflow
  name: nightly-report
  trigger:
    schedule:
      cron: "0 3 * * *"
```

Workflow agents are declarative orchestrations. The `trigger:` block is free-form -- consult the AgentSchema docs for the trigger types currently accepted by the Foundry runtime. There is no separate CLI verb for triggers; the schedule lives in the manifest.

---

## Model binding

For hosted agents that wrap a language model, point at a Foundry model deployment via the `resources:` array (manifest layout) or directly under the template:

```yaml
resources:
  - name: chat-model
    kind: model
    id: gpt-4.1-mini
```

The `id` matches a model deployment name on the Foundry project. List available deployments with:

```bash
azd ai agent connection list --output json
```

Model options (temperature, top_p, max output tokens, etc.) are configured on the deployed agent record, not in agent.yaml today -- they round-trip via `azd ai agent optimize apply` for tuned versions.

---

## Tools

Tools live under `resources:` with `kind: tool` and an `id` that names the tool kind. The supported `id` values map to the AgentSchema tool kinds the parser recognizes:

| Tool id              | What it is                                                              |
| -------------------- | ----------------------------------------------------------------------- |
| `function`           | Local function tool (custom JSON-schema parameters).                    |
| `custom`             | Generic server-side tool reached via a `Connection`.                    |
| `web_search`         | Web search (Bing default).                                              |
| `bing_grounding`     | Bing grounding (factual citations).                                     |
| `file_search`        | Vector-search over uploaded files in the agent's session filesystem.    |
| `mcp`                | MCP-protocol tool exposed by a remote MCP server. See note below.       |
| `openapi`            | Generic OpenAPI-backed tool.                                            |
| `code_interpreter`   | Sandboxed code execution.                                               |
| `azure_ai_search`    | Azure AI Search index lookup.                                           |
| `a2a_preview`        | Agent-to-Agent protocol bridge. Preview.                                |

Note on `mcp`: this is the AgentSchema tool kind -- it lets your AGENT CALL OUT to an MCP server. It is unrelated to `azd ai agent mcp start`, which exposes the azd ai agent CLI itself to IDEs over MCP. The two are not interchangeable.

Tool block shape (function example):

```yaml
resources:
  - name: lookup-user
    kind: tool
    id: function
    options:
      description: Look up a user by id.
      parameters:
        userId:
          schema: { type: string }
          required: true
          description: The user id to look up.
```

Tool kinds with secrets (e.g. `azure_ai_search`, `mcp` with an api key) pull credentials from a named connection. Add the connection to `resources:` with `kind: connection` and reference it in the tool's `options:` (see "Connections" below).

---

## Connections

Two ways to introduce a connection:

1. **Pre-existing** -- the connection already exists on the Foundry project. List with `azd ai agent connection list --output json` and reference it by name from a tool's `options:`.
2. **Declared in agent.yaml** -- the connection is created during `azd provision` by the Bicep pipeline. Add a `resources:` entry:

```yaml
resources:
  - name: search-index
    kind: connection
    category: CognitiveSearch
    target: https://my-search.search.windows.net/
    authType: ApiKey
    credentials:
      key: ${SEARCH_API_KEY}      # ${ENV_VAR} resolved at provision time
    metadata:
      indexName: docs-corpus
```

Required fields: `name`, `category`, `target`, `authType`. Optional: `credentials`, `metadata`, `audience` (for managed-identity auth types), `isSharedToAll`, `sharedUserList`.

Valid `category:` values (subset most often used): `AzureOpenAI`, `CognitiveSearch`, `CognitiveService`, `CustomKeys`, `ContainerRegistry`, `ApiKey`, `AzureBlob`, `Git`, `Redis`, `S3`, `Snowflake`, `AzureSqlDb`, `AzureMySqlDb`, `AzurePostgresDb`, `BingLLMSearch`, `MicrosoftOneLake`, `Elasticsearch`, `Pinecone`, `Qdrant`.

Valid `authType:` values: `AAD`, `ApiKey`, `CustomKeys`, `None`, `OAuth2`, `PAT`, `SAS`, `UserEntraToken`, `AgenticIdentity`, `ProjectManagedIdentity`, `ServicePrincipal`, `UsernamePassword`, `AccessKey`, `AccountKey`.

OAuth2 connections also accept `authorizationUrl`, `tokenUrl`, `refreshUrl`, `scopes`, and `connectorName`.

`${ENV_VAR}` placeholders inside `credentials:` are resolved at provision time from the active azd environment.

---

## Parameters (template variables)

The manifest layout supports `{{paramName}}` substitution. Declare parameters at the root:

```yaml
parameters:
  city:
    schema: { type: string }
    required: true
    description: City the agent answers questions about.
  units:
    schema: { type: string, enum: [metric, imperial], default: metric }

template:
  kind: hosted
  name: weather-agent
  description: "Weather for {{city}} in {{units}}."
```

Parameter values are supplied at `azd ai agent init` time (interactive or via flags) and frozen into the resulting file. The parser preserves the literal `{{...}}` tokens during round-trip, so re-deploying after edit does not re-prompt.

---

## Round-trip safety

When you edit `agent.yaml`:

* The parser preserves the layout (manifest vs. direct) on marshal/unmarshal so re-deploys do not rewrite the file shape.
* Unknown fields in the `template:` block surface in the deploy error with a precise path -- treat any "unknown field" as the strongest signal that the field name is mistyped or the AgentSchema reference has drifted from what this extension parses.
* Validate locally before deploy:

```bash
azd ai agent doctor --output json
```

Look for the `agent-yaml-valid` check; the failure message names the field path that failed validation.

---

## What this topic does NOT cover

* `azure.yaml` (azd project config) -- that file lists services, hosts, language, and the `startupCommand` for `azd ai agent run` (see `develop`).
* Creating connections at the Foundry project level via `azd ai agent connection create` -- see `configure`. (Use that for one-off connections you do NOT want to re-create on every `azd provision`.)
* Picking a model deployment / Foundry project -- those are arguments to `azd ai agent init` (see `initialize`) or `azd provision`.
* Deploying changes after edit -- see `deploy`.
