---
short: Recipes for adding toolboxes (web search, MCP, AI search, file search, OpenAPI, A2A, tool search).
order: 20
---
# Toolbox add: recipes

Each recipe shows the manifest fragment (init-time input), the resulting `azure.yaml` block (what you edit post-init), and any companion env vars or pre-created Foundry resources you need.

For the mental model and lifecycle, see `overview`. For the full tool-type reference, see `tools`. For connection setup, see `azd ai doc connection`.

## Apply pattern

After any recipe:

```bash
azd provision     # creates / updates connections referenced by toolbox tools
azd deploy        # creates a new toolbox version, updates TOOLBOX_<NAME>_MCP_ENDPOINT
azd ai agent invoke "..."     # smoke test
```

## Web search only (no connection)

Smallest possible toolbox. `web_search` uses Bing grounding by default; no connection needed.

Manifest:

```yaml
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

## Bing Custom Search

For a scoped Bing Custom Search instance:

```yaml
services:
  my-agent:
    config:
      connections:
        - name: bing-custom-conn
          category: GroundingWithCustomSearch
          authType: ApiKey
          target: ""
          credentials:
            key: ${PARAM_BING_CUSTOM_CONN_KEY}
          metadata:
            ResourceId: /subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Bing/accounts/<bing-account>
            type: bing_custom_search
      toolboxes:
        - name: search-tools
          tools:
            - type: bing_custom_search
              custom_search_configuration:
                instance_name: your-bing-instance
              project_connection_id: bing-custom-conn
```

```bash
azd env set PARAM_BING_CUSTOM_CONN_KEY "<bing-api-key>"
```

For plain `bing_grounding` (the legacy top-level tool, NOT inside a toolbox), see `azd ai doc connection add` -> "Bing grounding".

## Code interpreter + file search

```yaml
toolboxes:
  - name: agent-tools
    description: "Code execution + uploaded-file lookup."
    tools:
      - type: code_interpreter
      - type: file_search
        file_search:
          vector_store_ids:
            - ${FILE_SEARCH_VECTOR_STORE_ID}
```

`file_search` needs a pre-created vector store ID. Create one out-of-band:

```bash
# 1. Upload a file
curl -sS -X POST "$FOUNDRY_PROJECT_ENDPOINT/openai/v1/files" \
  -H "Authorization: Bearer $TOKEN" -F purpose=assistants -F file=@docs.txt
# 2. Create a vector store with the returned file ID
curl -sS -X POST "$FOUNDRY_PROJECT_ENDPOINT/openai/v1/vector_stores" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"file_ids":["file-..."]}'
# 3. Wire the vector store ID into the azd env
azd env set FILE_SEARCH_VECTOR_STORE_ID "vs_xxxxxxxxxxxx"
```

`code_interpreter` accepts optional `container.type: auto` + `container.file_ids[]` for pre-uploaded inputs.

Caveat (hosted): `code_interpreter` and `file_search` accessed through a toolbox do NOT support per-user isolation. All users in the project share the same container / vector store. Use the direct (non-toolbox) form if isolation matters.

## MCP server with a Personal Access Token (CustomKeys)

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
              server_url: https://api.githubcopilot.com/mcp
              project_connection_id: github-mcp-conn
```

```bash
azd env set PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION "Bearer ghp_xxx..."
```

For OAuth, UserEntraToken, AgenticIdentity, and other MCP auth flows, see `azd ai doc connection add`.

At deploy: the agents extension fills in `server_url` from the connection's `target` (if missing) and replaces `project_connection_id: github-mcp-conn` with the connection's ARM resource ID.

## Azure AI Search inside a toolbox

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
      toolboxes:
        - name: agent-tools
          tools:
            - type: azure_ai_search
              index_name: contoso-outdoors
              project_connection_id: my-search-conn
              # Optional: top_k (default 5), query_type (default vector_semantic_hybrid),
              # filter (applies to all queries).
```

```bash
azd env set PARAM_MY_SEARCH_CONN_KEY "<search-admin-key>"
```

`index_name` is required on the tool entry (the connection just holds the search service endpoint + auth). For multiple indexes, add multiple `azure_ai_search` entries with unique `name:` fields. For the connection-only side (azure.yaml `resources[]` instead of a toolbox tool), see `azd ai doc connection add` -> "Azure AI Search RAG".

## OpenAPI tool (key auth)

The OpenAPI tool has a different shape from MCP -- everything nests under an `openapi:` key.

```yaml
services:
  my-agent:
    config:
      connections:
        - name: api-conn
          category: CustomKeys
          authType: CustomKeys
          target: https://api.example.com
          credentials:
            keys:
              key: ${PARAM_API_CONN_KEY}
      toolboxes:
        - name: openapi-tools
          tools:
            - type: openapi
              openapi:
                name: my-api
                spec:
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
                  type: connection_auth
                  connection_id: api-conn
```

```bash
azd env set PARAM_API_CONN_KEY "<api-key>"
```

For other auth shapes (`anonymous`, `managed_identity`), see `tools` -> OpenAPI.

## A2A peer agent

```yaml
services:
  my-agent:
    config:
      connections:
        - name: a2a-conn
          category: RemoteA2A
          authType: None
          target: https://your-remote-agent.azurecontainerapps.io
      toolboxes:
        - name: a2a-tools
          tools:
            - type: a2a_preview
              project_connection_id: a2a-conn
```

For an authenticated peer, use `RemoteTool` + `ProjectManagedIdentity` (with `audience:`) instead. No env vars needed for `None` or `ProjectManagedIdentity`.

## Multi-tool toolbox

Mix freely. Add a `description` to every tool so the model can pick:

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
      toolboxes:
        - name: agent-tools
          description: "GitHub MCP + AI Search + web search + code execution."
          tools:
            - type: mcp
              server_label: github
              project_connection_id: github-conn
              description: GitHub repo operations.
            - type: azure_ai_search
              index_name: contoso-outdoors
              project_connection_id: my-search-conn
              description: Internal docs corpus.
            - type: web_search
              description: General web search.
            - type: code_interpreter
              description: Run Python for data analysis.
```

## Multiple instances of the same built-in type

Foundry's toolbox API allows at most ONE built-in tool of a given type without a `name`. Give each instance a unique `name:` and a discriminating `description:`:

```yaml
toolboxes:
  - name: agent-tools
    tools:
      - type: azure_ai_search
        name: product-search
        description: Search the product catalog.
        index_name: products
        project_connection_id: my-search-conn
      - type: azure_ai_search
        name: support-search
        description: Search support tickets.
        index_name: support
        project_connection_id: my-search-conn
```

Without unique names the API returns `400 invalid_payload: Multiple tools without identifiers found...`.

## Tool Search (intent-based routing)

For large toolboxes, let the platform pick the most relevant tools per request instead of exposing every tool to the model:

```yaml
toolboxes:
  - name: agent-tools
    tools:
      - type: toolbox_search_preview
      - type: web_search
      - type: mcp
        server_label: github
        project_connection_id: github-mcp-conn
      # ... many more tools
```

`toolbox_search_preview` is a directive -- it doesn't appear in `tools/list` and doesn't count toward the one-unnamed-per-type limit. No extra configuration needed.

## Remove a tool from a toolbox

1. Remove the entry from `toolboxes[].tools[]` in `azure.yaml`.
2. If no other tool references the connection, remove it from `connections[]`.
3. `azd env unset PARAM_<...>` for any orphaned credential env vars.
4. `azd deploy` -- creates a new toolbox version without the tool.

To remove an entire toolbox: drop the `toolboxes[]` entry and `azd deploy`. The toolbox stays on Foundry (azd doesn't delete it); clean it up via the Foundry Toolkit, SDK, or REST API.

## Validate

```bash
azd ai agent doctor --output json
```

Watch the `provisionToolboxes` output streamed to stderr during `azd deploy` -- a per-toolbox "Provisioning toolbox: X" / "Toolbox 'X' provisioned" pair confirms each one POSTed successfully.
