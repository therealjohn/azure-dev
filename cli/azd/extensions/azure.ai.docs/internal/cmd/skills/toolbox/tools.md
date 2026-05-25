---
short: Reference of tool types accepted by a toolbox.
order: 30
---
# Toolbox tool types

Reference for entries inside `azure.yaml services.<name>.config.toolboxes[].tools[]`. Three flavors: **built-in** (no connection), **connection-bound** (need `project_connection_id`), and **custom** (need an endpoint + connection). Plus one directive (`toolbox_search_preview`) that changes how tools are surfaced.

For recipes, see `add`. For connection setup, see `azd ai doc connection`.

## Built-in tools

Pre-configured capabilities Foundry runs for you. Drop them into `tools[]` with just a `type:` (and optional `name:` / `description:`).

| `type:`            | Optional fields                                                          | What it does                                                              |
| ------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| `web_search`       | `name`, `description`, `web_search.custom_search_configuration`          | Web search via Bing grounding by default. Add a custom Bing Search connection through `custom_search_configuration` if you have one. |
| `code_interpreter` | `name`, `description`, `container.type` (`auto`), `container.file_ids[]` | Sandboxed Python execution.                                               |
| `file_search`      | `name`, `description`, **`file_search.vector_store_ids[]` (required)**   | Vector search over a pre-created vector store. The IDs must reference vector stores already in the Foundry project (create them via `POST {project_endpoint}/openai/v1/vector_stores`). |
| `function`         | `name`, `description`, JSON-Schema `parameters`                          | Local function the agent calls; your application executes it.             |

Caveat: a toolbox supports at most ONE built-in tool of a given `type:` without a `name:`. To include multiple instances of the same type, give each a unique `name`.

Caveat (hosted): `code_interpreter` and `file_search` do NOT support per-user isolation when accessed through a toolbox in a Hosted agent. All users in the same project share the same container / vector store. Use the direct (non-toolbox) tool form when isolation matters.

## Connection-bound built-ins

Same shape as built-in, but Foundry needs a connection for credentials / endpoint.

### `type: azure_ai_search`

```yaml
- type: azure_ai_search
  name: my-search                 # optional
  description: Search the docs corpus.
  index_name: contoso-outdoors    # required
  project_connection_id: my-search-conn   # required
  # Optional: top_k (default 5), query_type (default vector_semantic_hybrid),
  # filter (applies to all queries).
```

Required: `type`, `index_name`, `project_connection_id`. Connection category: `CognitiveSearch`. `query_type` accepts `simple`, `vector`, `semantic`, `vector_simple_hybrid`, `vector_semantic_hybrid`.

Results include chunk metadata at `result.structuredContent.documents[]` (`title`, `url`, `id`, `score`).

### `type: bing_custom_search`

```yaml
- type: bing_custom_search
  custom_search_configuration:
    instance_name: your-bing-custom-instance
  project_connection_id: bing-custom-conn
```

For a scoped Bing Custom Search instance. Connection category: `GroundingWithCustomSearch` (or `GroundingWithBingSearch` for a non-custom instance). Plain web search (no custom instance) uses `type: web_search` and needs no connection.

`bing_grounding` is NOT a valid toolbox tool type -- it only works at the agent's top level (a `resources[]` entry with `{ resource: bing_grounding, connectionName: <name> }`; see `azd ai doc agent configure`).

## Custom tools

Bring your own endpoint. The connection holds the URL + auth; Foundry injects credentials at call time.

### `type: mcp`

```yaml
- type: mcp
  server_label: github                       # short identifier the model sees
  server_url: https://api.example.com/mcp    # required for unauthenticated; auto-filled from connection.target at deploy when project_connection_id is set
  project_connection_id: github-mcp-conn     # omit for public / anonymous MCP
  description: GitHub repo operations
  require_approval: never                    # "always" or "never"
  allowed_tools: [search, get_file]          # optional allowlist of MCP tool names
```

Connection category: usually `RemoteTool`. Supported auth types: `CustomKeys` (API key in header), `OAuth2` (managed connector or your own app), `AgenticIdentity`, `UserEntraToken`, `None`. See `azd ai doc connection add` for per-auth recipes.

**Important about `require_approval`**: the toolbox MCP proxy does NOT enforce this. It's a directive your agent runtime must read from each tool's `_meta.tool_configuration` block and gate the call accordingly. `"always"` -> ask the user before every invocation; `"never"` -> invoke freely. The first OAuth call returns `CONSENT_REQUIRED` (code `-32007`) with a consent URL -- the agent runtime opens it in a browser and retries.

**Tool name prefixing**: at runtime, MCP tool names are prefixed with `server_label`. A tool `get_info` on `server_label: myserver` is exposed as `myserver.get_info`. (The Copilot SDK rejects dots and replaces them with underscores -- `myserver_get_info`. Other runtimes pass dots through.)

`mcp` here means YOUR agent calls out to an MCP server. It is NOT related to `azd ai agent mcp start`, which exposes the CLI itself to IDEs.

### `type: openapi`

```yaml
- type: openapi
  openapi:                                   # all OpenAPI config nests under this key
    name: my-api
    spec:                                    # the full OpenAPI 3.0 / 3.1 spec, inline
      openapi: "3.0.1"
      info: { title: "My API", version: "1.0" }
      servers: [{ url: https://api.example.com/v1 }]
      paths:
        /search:
          get:
            operationId: search
            parameters:
              - { name: query, in: query, required: true, schema: { type: string } }
            responses:
              "200": { description: OK }
    auth:
      type: connection_auth                  # or "anonymous" / "managed_identity"
      connection_id: api-conn
```

Required: `type`, `openapi.name`, `openapi.spec`, `openapi.auth.type`. Connection category for `connection_auth` typically `CustomKeys` or `OAuth2`.

`auth.type` values:

* `anonymous` -- no auth.
* `connection_auth` -- pulls credentials from `connection_id` (a Foundry connection name).
* `managed_identity` -- needs `security_scheme.audience`. The Foundry project's managed identity calls the API; grant it the right RBAC role on the target service first.

### `type: a2a_preview`

```yaml
- type: a2a_preview
  name: calc-agent                           # optional
  description: Hand off complex math.
  project_connection_id: peer-agent-conn
  # Optional: base_url override (otherwise sourced from the connection's target).
```

Required: `type`, `project_connection_id`. Connection category: `RemoteA2A` (or `RemoteTool`). Common auth types: `None` (anonymous), `ProjectManagedIdentity`, `AgenticIdentity`.

## Tool Search directive

```yaml
- type: toolbox_search_preview
```

Activates intent-based tool routing. The platform picks the most relevant subset of the toolbox's tools for each request instead of exposing every tool to the model at once. Doesn't appear in `tools/list` responses; doesn't count toward the one-unnamed-per-type limit.

## Universal optional fields

| Field         | What it does                                                                                                |
| ------------- | ----------------------------------------------------------------------------------------------------------- |
| `name`        | Unique within the toolbox. Required when including multiple instances of the same built-in `type`.          |
| `description` | Free-text -- the MODEL reads this to choose between tools. Always add one when the toolbox has more than one tool. |

## What azd fills in at deploy

`provisionToolboxes` (post-deploy hook) walks each tool entry and:

1. Resolves `${VAR}` references in every string value against the active azd environment.
2. For tools with `project_connection_id`: fills in `server_url` from the matching connection's `target` (if not already set) and `server_label` from the connection's `name` (if not already set).
3. Replaces `project_connection_id` (a friendly name) with the connection's ARM resource ID (from Bicep output `AI_PROJECT_CONNECTION_IDS_JSON`).
4. POSTs the resulting tool list to `{project_endpoint}/toolboxes/{name}/versions?api-version=v1` with the `Foundry-Features: Toolboxes=V1Preview` header.

You can write minimal entries in `azure.yaml` -- `type` + `project_connection_id` is enough for `mcp` / `a2a_preview` -- and the deploy fills the rest in.

## Reference

* Foundry tool catalog: https://learn.microsoft.com/en-us/azure/foundry/agents/concepts/tool-catalog
* Toolbox how-to: https://learn.microsoft.com/en-us/azure/foundry/agents/how-to/tools/toolbox
