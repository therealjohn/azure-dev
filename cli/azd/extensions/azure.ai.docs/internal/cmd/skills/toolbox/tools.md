---
short: Reference of tool types accepted by a toolbox.
order: 30
---
# Toolbox tool types

Reference for entries inside `azure.yaml services.<name>.config.toolboxes[].tools[]`. Two flavors: **built-in** (no connection) and **custom** (needs `project_connection_id` pointing at a connection in the same service config).

For recipes, see `add`. For connection setup, see `azd ai doc connection`.

## Built-in tools

Pre-configured capabilities Foundry runs for you. Drop them into `tools[]` with just a `type:` (and optional `name:` / `description:`).

| `type:`            | Fields                                                    | What it does                                                            |
| ------------------ | --------------------------------------------------------- | ----------------------------------------------------------------------- |
| `web_search`       | `name?`, `description?`                                   | Bing web search; returns answers with inline citations.                 |
| `code_interpreter` | `name?`, `description?`                                   | Sandboxed Python execution. Useful for data analysis and charts.        |
| `file_search`      | `name?`, `description?`                                   | Vector-search over files in the agent's session filesystem.             |
| `function`         | `name`, `description`, JSON-Schema `parameters`           | Local function the agent calls; your application executes the function. |

Caveat: a toolbox supports at most ONE built-in tool of a given `type:` without a `name:`. To include multiple `web_search` (or any other) instances, give each a unique `name`.

## Connection-bound built-ins

Same idea as built-in, but Foundry needs a connection to know which Azure resource to query. The connection must exist on the project (declared in `connections[]` or pre-existing).

| `type:`           | Required fields                                            | Connection category   | Notes                                                            |
| ----------------- | ---------------------------------------------------------- | --------------------- | ---------------------------------------------------------------- |
| `azure_ai_search` | `project_connection_id`                                    | `CognitiveSearch`     | Index name lives in connection `metadata.indexName`.             |
| `bing_grounding`  | `project_connection_id`                                    | `GroundingWithBingSearch` | More structured than `web_search`; gives Bing-style grounding citations. |

Both can also live outside a toolbox as a top-level `resources[]` entry (`{ resource, connectionName }`) -- equivalent behavior. Pick the toolbox form when you want all tools discoverable through the same MCP endpoint.

## Custom tools

Bring your own endpoint. Each requires a connection reference; the connection holds the URL + auth and Foundry injects credentials at call time.

### `type: mcp`

Most common custom tool. Connects to an MCP server.

```yaml
- type: mcp
  server_label: github                   # short identifier the model sees
  server_url: https://api.example.com/mcp   # OPTIONAL -- auto-filled from connection.target at deploy
  project_connection_id: github-mcp-conn
  description: GitHub repo operations
  require_approval: never                # or "always" / detailed approval policy
  allowed_tools: [search, get_file]      # optional allowlist of MCP tool names
```

Required: `type`, `project_connection_id`. Everything else is optional. Connection category is usually `RemoteTool`.

`mcp` here means YOUR agent calls out to an MCP server. It is NOT related to `azd ai agent mcp start`, which exposes the CLI itself to IDEs.

### `type: openapi`

Generic HTTP API described by an OpenAPI 3.0 / 3.1 spec.

```yaml
- type: openapi
  project_connection_id: contoso-api-conn
  description: Contoso billing API
  # The OpenAPI spec file lives in your agent source and gets uploaded at deploy.
```

Required: `type`, `project_connection_id`. Connection category depends on the API's auth (`ApiKey`, `CustomKeys`, `OAuth2`).

### `type: a2a_preview`

Delegate to another deployed agent over the A2A protocol.

```yaml
- type: a2a_preview
  project_connection_id: peer-agent-conn
  description: Hand off complex math to the calc-agent.
```

Required: `type`, `project_connection_id`. Connection category usually `RemoteTool` with `ProjectManagedIdentity` auth.

### `type: tool_search`

Searches a registry of tools and dynamically loads matches -- helpful when an agent shouldn't preload every tool. Newer / less common; consult the Foundry tool catalog for current field set.

## Universal fields

Every tool entry accepts these regardless of `type`:

| Field         | What it does                                                                                       |
| ------------- | -------------------------------------------------------------------------------------------------- |
| `name`        | Unique identifier within the toolbox. Required when including multiple built-ins of the same type. |
| `description` | Free-text -- the MODEL reads this to choose between tools. Always add one when the toolbox has more than one tool. |

## What azd does to your tool entry at deploy

`provisionToolboxes` (called as a post-deploy hook) walks each tool and:

1. Resolves `${VAR}` references in every string value against the azd environment.
2. For tools with `project_connection_id`: fills in `server_url` from the matching connection's `target` (if not already set) and `server_label` from the connection's `name` (if not already set).
3. Replaces `project_connection_id` (a friendly name) with the connection's ARM resource ID (resolved from the Bicep output `AI_PROJECT_CONNECTION_IDS_JSON`).
4. POSTs the resulting tool list to `{project}/toolboxes/{name}/versions?api-version=v1` with the `Foundry-Features: Toolboxes=V1Preview` header.

This means you can write minimal entries in `azure.yaml` -- `type` + `project_connection_id` is enough for `mcp` / `openapi` / `a2a_preview` -- and the deploy fills the rest in.

## Reference

* Foundry tool catalog: https://learn.microsoft.com/en-us/azure/foundry/agents/concepts/tool-catalog
* Toolbox how-to: https://learn.microsoft.com/en-us/azure/foundry/agents/how-to/tools/toolbox
