---
short: Recipes for adding toolboxes (web search, MCP, multi-tool, multi-instance).
order: 20
---
# Toolbox add: recipes

Each recipe shows the manifest fragment (init-time input), the resulting `azure.yaml` block (what you edit post-init), and any companion files (connections, env vars) you need.

For the mental model and lifecycle, see `overview`. For the full tool-type reference, see `tools`. For connection setup (categories, auth types, credentials), see `azd ai doc connection`.

## Apply pattern

After any recipe:

```bash
azd provision     # creates / updates connections referenced by toolbox tools
azd deploy        # creates a new toolbox version, updates TOOLBOX_<NAME>_MCP_ENDPOINT
azd ai agent invoke "..."     # smoke test
```

## Web search only (no connection)

Smallest possible toolbox. `web_search` is a built-in tool with Bing grounding default; no connection needed.

Manifest:

```yaml
template:
  kind: hosted
  ...
  environment_variables:
    - name: TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT
      value: ${TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT}

resources:
  - kind: toolbox
    name: agent-tools
    tools:
      - type: web_search
```

azure.yaml:

```yaml
services:
  my-agent:
    config:
      toolboxes:
        - name: agent-tools
          tools:
            - type: web_search
```

No connection, no env vars. Init adds the `TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT` reference to the on-disk `agent.yaml` automatically.

## Code interpreter + file search

Two more built-in tools that need no connections. Drop them straight into the same toolbox:

```yaml
toolboxes:
  - name: agent-tools
    description: "Web research + code execution + uploaded-file lookup."
    tools:
      - type: web_search
      - type: code_interpreter
      - type: file_search
```

`file_search` operates over files uploaded to the agent's session filesystem -- see `azd ai doc agent configure` -> File uploads.

## MCP server with a connection

The most common custom tool. Pair a `kind: connection` (in `connections[]`) with a toolbox tool of `type: mcp` that references it.

```yaml
services:
  my-agent:
    config:
      connections:
        - name: github-mcp-conn
          category: RemoteTool
          target: https://api.githubcopilot.com/mcp
          authType: CustomKeys
          credentials:
            keys:
              Authorization: ${PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION}
      toolboxes:
        - name: agent-tools
          tools:
            - type: mcp
              server_label: github
              project_connection_id: github-mcp-conn
              # server_url is auto-filled at deploy from the connection's target
```

Env vars:

```bash
azd env set PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION "Bearer ghp_xxx..."
```

For OAuth2, UserEntraToken, AgenticIdentity, and other auth recipes, see `azd ai doc connection add`.

At deploy time, the agents extension:

1. Looks up `github-mcp-conn` in `connections[]` / `toolConnections[]`.
2. Fills in `server_url` from the connection's `target` (if not already set).
3. Replaces `project_connection_id: github-mcp-conn` with the ARM resource ID resolved from Bicep output.

## Azure AI Search inside a toolbox

`azure_ai_search` works as a toolbox tool when you want it discovered through the same MCP endpoint as the rest. It still needs a `CognitiveSearch` connection.

```yaml
services:
  my-agent:
    config:
      connections:
        - name: my-search-conn
          category: CognitiveSearch
          target: https://my-search.search.windows.net/
          authType: ApiKey
          credentials:
            key: ${PARAM_MY_SEARCH_CONN_KEY}
          metadata:
            indexName: contoso-outdoors
      toolboxes:
        - name: agent-tools
          tools:
            - type: azure_ai_search
              project_connection_id: my-search-conn
```

```bash
azd env set PARAM_MY_SEARCH_CONN_KEY "<search-admin-key>"
```

Alternative -- bind it as a top-level resource instead of through a toolbox. Use the `resources[]` block from `configure`:

```yaml
resources:
  - resource: azure_ai_search
    connectionName: my-search-conn
```

Pick the toolbox form when you want the agent's MCP layer to discover the index; pick `resources[]` when you want classic direct binding (mostly equivalent in behavior).

## Multi-tool toolbox

Mix built-in + custom freely. Tools are surfaced to the agent in the order listed.

```yaml
services:
  my-agent:
    config:
      connections:
        - name: github-conn
          category: RemoteTool
          target: https://api.githubcopilot.com/mcp
          authType: CustomKeys
          credentials:
            keys:
              Authorization: ${PARAM_GITHUB_CONN_KEYS_AUTHORIZATION}
        - name: my-search-conn
          category: CognitiveSearch
          target: https://my-search.search.windows.net/
          authType: ApiKey
          credentials:
            key: ${PARAM_MY_SEARCH_CONN_KEY}
          metadata:
            indexName: contoso-outdoors
      toolboxes:
        - name: agent-tools
          description: "GitHub MCP + AI Search + web search + code execution."
          tools:
            - type: mcp
              server_label: github
              project_connection_id: github-conn
            - type: azure_ai_search
              project_connection_id: my-search-conn
            - type: web_search
            - type: code_interpreter
```

## Multiple instances of the same built-in type

Foundry's toolbox API allows at most **one** built-in tool of a given type without a `name`. To include two instances of `web_search` (e.g. one general, one custom-scoped), give each a unique `name:` and `description:`:

```yaml
toolboxes:
  - name: agent-tools
    tools:
      - type: web_search
        name: general_search
        description: General web search across the open web.
      - type: web_search
        name: docs_search
        description: Search restricted to internal documentation sites.
```

Without unique names, the API returns `invalid_payload`.

Tip: add a `description:` to every tool in a toolbox. The model uses these to pick the right tool when more than one could plausibly answer the request.

## OpenAPI tool

```yaml
services:
  my-agent:
    config:
      connections:
        - name: contoso-api-conn
          category: ApiKey
          target: https://api.contoso.com
          authType: ApiKey
          credentials:
            key: ${PARAM_CONTOSO_API_CONN_KEY}
      toolboxes:
        - name: agent-tools
          tools:
            - type: openapi
              project_connection_id: contoso-api-conn
              # The OpenAPI spec lives in your agent source and gets uploaded at deploy time.
```

```bash
azd env set PARAM_CONTOSO_API_CONN_KEY "<api-key>"
```

## A2A peer agent

```yaml
services:
  my-agent:
    config:
      connections:
        - name: peer-agent-conn
          category: RemoteTool
          target: https://other-agent.foundry-account.westus2.azure.com/
          authType: ProjectManagedIdentity
          audience: https://ai.azure.com/.default
      toolboxes:
        - name: agent-tools
          tools:
            - type: a2a_preview
              project_connection_id: peer-agent-conn
```

No env vars -- the project's managed identity calls the peer.

## Remove a tool from a toolbox

1. Remove the entry from `toolboxes[].tools[]` in `azure.yaml`.
2. If no other tool references the connection, remove it from `connections[]` / `toolConnections[]`.
3. `azd env unset PARAM_<...>` for any orphaned credential env vars.
4. `azd deploy` -- creates a new toolbox version without the tool.

To remove an entire toolbox, drop the whole `toolboxes[]` entry and `azd deploy`. The toolbox stays on Foundry (azd doesn't delete it); use the Foundry Toolkit or REST API if you need to clean it up there.

## Validate

```bash
azd ai agent doctor --output json
```

Look for the `local.toolboxes-valid` check (when present) and the deploy-time `provisionToolboxes` output streamed to stderr.
