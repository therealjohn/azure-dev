---
short: Mental model for Foundry connections (declarative vs. imperative, how they wire to tools).
order: 10
---
# Connection overview

Audience: an AI coding assistant adding, editing, or removing a Foundry project connection in an `azd ai agent` project. This topic establishes the mental model. For end-to-end recipes (GitHub MCP, Azure AI Search, Bing, OpenAPI), see `add`. For the imperative CLI, see `manage`.

---

## What a connection is

A Foundry **connection** is a project-level resource that holds the endpoint URL + auth credentials for an external service: an MCP server, an OpenAPI backend, an Azure AI Search index, a Bing search account, an A2A peer agent, a container registry, an AI service. Connections are stored on the Foundry project (NOT on the agent) and referenced by name from tool definitions.

When a tool that needs a connection runs (e.g. an `mcp` tool inside a toolbox), the Foundry runtime injects the connection's credentials at call time. The agent code never sees the secret; deployed `agent.yaml` never embeds it.

A connection has four required pieces and a handful of optional ones:

* `name` -- unique within the project. What tools reference (`project_connection_id`).
* `category` -- what kind of thing it points at (`RemoteTool` for MCP, `CognitiveSearch` for Azure AI Search, `OAuth2` for OAuth-protected APIs, etc.). See `categories`.
* `target` -- the URL or ARM resource ID.
* `authType` -- how the runtime authenticates to it (`ApiKey`, `CustomKeys`, `OAuth2`, `UserEntraToken`, `AgenticIdentity`, `ProjectManagedIdentity`, `None`). See `auth-types`.
* Optional: `credentials`, `metadata`, `audience`, OAuth2 endpoints, `connectorName`, sharing flags.

---

## Three ways a connection comes into existence

| Path | Where the connection definition lives | When to use |
| ---- | ------------------------------------- | ----------- |
| **Declarative -- via azd** | `azure.yaml` `services.<name>.config.connections[]` (or `toolConnections[]`) | Project-owned connections that should be re-created on every `azd provision` against a fresh environment. The default and recommended path. Init scaffolds this when you pass an `agent.manifest.yaml` with a `kind: connection` resource. |
| **Pre-existing on Foundry** | Nowhere local. Created out-of-band (portal, another team's deploy). | Shared connections owned by another team. Reference by name from `agent.yaml`'s toolbox `tools[]` (`project_connection_id`) or `resources[]` (`connectionName`); don't redeclare in `azure.yaml`. |
| **Imperative -- via CLI** | Nowhere local. Created by `azd ai agent connection create ...` directly against the Foundry project. | One-off shared dev connections; quick experiments; manually wiring a connection you don't want to re-create on every provision. See `manage`. |

The three paths can coexist on the same project. A toolbox tool referencing `project_connection_id: my-conn` doesn't care which path created `my-conn`; it just needs the name to resolve.

### Imperative caveat

Connections created with `azd ai agent connection create` survive `azd down` (they live on the Foundry project, not in your azd environment). They are NOT re-created by `azd provision`. If you nuke and rebuild the environment, anything imperative has to be re-issued. Declarative is more reproducible.

---

## How connections wire to tools

A connection on its own does nothing. It gets ACTIVATED when a tool references it. Three patterns:

### Pattern 1: Toolbox tool with `project_connection_id`

The modern, recommended grouping. Used for `mcp`, `openapi`, `a2a_preview`, and for `azure_ai_search` / `bing_grounding` inside a toolbox.

```yaml
# azure.yaml services.<name>.config
connections:
  - name: github-mcp-conn
    category: RemoteTool
    target: https://api.githubcopilot.com/mcp
    authType: CustomKeys
    credentials:
      Authorization: ${PARAM_GITHUB_MCP_CONN_AUTHORIZATION}
toolboxes:
  - name: agent-tools
    tools:
      - type: mcp
        server_label: github
        project_connection_id: github-mcp-conn   # <-- wires the connection in
```

### Pattern 2: `resources[]` with `connectionName` (built-in tools that need a connection)

For `bing_grounding` and `azure_ai_search` at the agent's top level (NOT inside a toolbox). This is the older direct-binding pattern that `azd ai agent init` writes when the manifest has `kind: tool, id: bing_grounding | azure_ai_search`.

```yaml
# azure.yaml services.<name>.config
connections:
  - name: my-search-conn
    category: CognitiveSearch
    target: https://my-search.search.windows.net/
    authType: ApiKey
    credentials:
      key: ${PARAM_MY_SEARCH_CONN_KEY}
    metadata:
      indexName: docs-corpus
resources:
  - resource: azure_ai_search
    connectionName: my-search-conn               # <-- wires the connection in
```

### Pattern 3: Built-in tools with no connection

Some built-in tools don't need a connection at all: `web_search` (Bing-default), `code_interpreter`, `file_search` (uses uploaded files), `function` (local). These appear in toolboxes as bare `type:` entries with no `project_connection_id`.

```yaml
toolboxes:
  - name: misc-tools
    tools:
      - type: web_search
      - type: code_interpreter
```

---

## Credential externalization (PARAM_*)

You'll see `${PARAM_<CONN>_<KEY>}` patterns throughout `azure.yaml` connection blocks. This is intentional -- raw secrets MUST NOT be committed to `azure.yaml`.

When init reads a manifest connection's `credentials:` map, it walks every string leaf and:

1. Stores the raw value as an azd env var named `PARAM_<UPPER_CONN_NAME>_<UPPER_KEY>` (non-alphanumeric replaced with `_`).
2. Rewrites the value in `azure.yaml` to `${PARAM_<...>}` so the Bicep pipeline reads the env var at provision time.

When you ADD a connection manually post-init, you do the same thing:

* Edit `azure.yaml` to add the connection block, using `${PARAM_<name>}` placeholders for any credential string.
* `azd env set PARAM_<NAME> <value>` for each one.

The full rule (nested-map handling, the `credentials.type` -> top-level `authType` promotion, every auth-type's expected credential shape) lives in `auth-types`.

---

## When to consult which topic

* **"What kind of thing am I connecting to?"** -- `categories`
* **"How do I structure credentials for this auth type?"** -- `auth-types`
* **"Step-by-step for my exact scenario (GitHub MCP, Azure AI Search, Bing, OpenAPI, A2A)"** -- `add`
* **"I just want the CLI for create/update/delete"** -- `manage`
* **"How do connection blocks fit in azure.yaml overall?"** -- `azd ai doc agent configure`
