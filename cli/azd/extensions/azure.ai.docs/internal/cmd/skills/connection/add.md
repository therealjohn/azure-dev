---
short: Recipes for adding common connections (MCP, Azure AI Search, Bing, OpenAPI, A2A).
order: 20
---
# Connection add: recipes by scenario

Audience: an AI coding assistant adding a specific connection to an existing `azd ai agent` project. Pick the recipe matching the user's intent. Each recipe shows the manifest fragment (for seed-time init), the resulting `azure.yaml` shape (for manual post-init edits), and the env vars that need setting.

For the mental model behind the three paths (declarative / pre-existing / imperative), read `overview` first.

---

## How to use these recipes

Each recipe has three parts:

* **Manifest fragment** -- what you'd put in an `agent.manifest.yaml` if you were doing this at init time (`azd ai agent init -m <manifest>`). Useful as input for a fresh project.
* **Resulting `azure.yaml`** -- what init produces. THIS is what you edit when adding the connection POST-init. Drop it under `services.<name>.config:`.
* **Env vars** -- the `PARAM_*` values that need to be set with `azd env set` for `azd provision` to pick up the credentials.

After applying any recipe:

```bash
azd provision   # Bicep creates the connection on the Foundry project
azd deploy      # publishes / updates the toolbox referencing the connection (if any)
azd ai agent invoke "..."   # smoke test
```

---

## Recipe: GitHub MCP via Personal Access Token (CustomKeys)

User intent: "Add GitHub MCP as a tool with my PAT for auth."

### Manifest fragment

```yaml
parameters:
  github_pat:
    secret: true
    description: GitHub Personal Access Token (classic ghp_... or fine-grained github_pat_...)

resources:
  - kind: connection
    name: github-mcp-conn
    target: https://api.githubcopilot.com/mcp
    category: RemoteTool
    credentials:
      type: CustomKeys
      keys:
        Authorization: "Bearer {{ github_pat }}"
  - kind: toolbox
    name: agent-tools
    tools:
      - type: mcp
        server_label: github
        server_url: https://api.githubcopilot.com/mcp
        project_connection_id: github-mcp-conn
```

### Resulting `azure.yaml`

```yaml
services:
  my-agent:
    config:
      connections:
        - name: github-mcp-conn
          category: RemoteTool
          target: https://api.githubcopilot.com/mcp
          authType: CustomKeys   # promoted from credentials.type
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
              # `target` / `authType` / `credentials` from the manifest tool entry
              # are removed by init (they're hoisted into the connection above);
              # `server_label`, `server_url`, and `project_connection_id` are kept.
```

### Env vars

```bash
azd env set PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION "Bearer ghp_xxx..."
```

---

## Recipe: GitHub MCP via Foundry-managed OAuth2

User intent: "Add GitHub MCP without handing me PATs -- let Microsoft manage the OAuth app."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: github-oauth-conn
    category: RemoteTool
    authType: OAuth2
    target: https://api.githubcopilot.com/mcp
    connectorName: foundrygithubmcp     # Microsoft-managed OAuth app
    credentials:
      type: OAuth2
  - kind: toolbox
    name: agent-tools
    tools:
      - type: mcp
        server_label: github
        project_connection_id: github-oauth-conn
```

### Resulting `azure.yaml`

```yaml
services:
  my-agent:
    config:
      connections:
        - name: github-oauth-conn
          category: RemoteTool
          target: https://api.githubcopilot.com/mcp
          authType: OAuth2
          connectorName: foundrygithubmcp
      toolboxes:
        - name: agent-tools
          tools:
            - type: mcp
              server_label: github
              project_connection_id: github-oauth-conn
```

### Env vars

None for Foundry-managed OAuth -- the platform handles the client credentials. End users authorize the connection on first call via the standard OAuth2 consent flow.

---

## Recipe: MCP server with user-on-behalf-of (UserEntraToken)

User intent: "Add an MCP server that uses the END USER'S Entra token (1P OBO flow). The agent acts as the user, not as itself."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: workiq-mail-conn
    category: RemoteTool
    authType: UserEntraToken
    audience: ea9ffc3e-8a23-4a7d-836d-234d7c7565c1   # Required: token audience (the MCP server's app ID)
    target: https://agent365.svc.cloud.microsoft/agents/servers/mcp_MailTools
  - kind: toolbox
    name: agent-tools
    tools:
      - type: mcp
        server_label: workiq-mail
        project_connection_id: workiq-mail-conn
```

### Resulting `azure.yaml`

```yaml
services:
  my-agent:
    config:
      connections:
        - name: workiq-mail-conn
          category: RemoteTool
          target: https://agent365.svc.cloud.microsoft/agents/servers/mcp_MailTools
          authType: UserEntraToken
          audience: ea9ffc3e-8a23-4a7d-836d-234d7c7565c1
      toolboxes:
        - name: agent-tools
          tools:
            - type: mcp
              server_label: workiq-mail
              project_connection_id: workiq-mail-conn
```

### Env vars

None. The user's token is supplied at call time (Entra ID handles the on-behalf-of exchange). This auth type requires that the user invoking the agent has been granted the right Entra roles on the target MCP server.

---

## Recipe: Azure AI Search RAG

User intent: "Ground my agent's answers in an Azure AI Search index."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: my-search-conn
    category: CognitiveSearch
    target: https://my-search.search.windows.net/
    authType: ApiKey
    credentials:
      key: "{{ search_api_key }}"
    metadata:
      indexName: contoso-outdoors
  - kind: tool
    id: azure_ai_search
    name: search
```

### Resulting `azure.yaml`

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
      resources:
        - resource: azure_ai_search
          connectionName: my-search-conn   # init PROMPTS for this name during the kind:tool path
```

### Env vars

```bash
azd env set PARAM_MY_SEARCH_CONN_KEY "<search-admin-key>"
```

Alternative: use `authType: AAD` and the agent's managed identity (no API key, no env var). Grant the agent's managed identity the `Search Index Data Reader` role on the search service. See `auth-types`.

---

## Recipe: Bing grounding

User intent: "Add Bing grounding so my agent can cite real web sources."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: bing-grounding-conn
    category: GroundingWithBingSearch
    target: https://api.bing.microsoft.com/
    authType: ApiKey
    credentials:
      key: "{{ bing_api_key }}"
  - kind: tool
    id: bing_grounding
    name: bing
```

### Resulting `azure.yaml`

```yaml
services:
  my-agent:
    config:
      connections:
        - name: bing-grounding-conn
          category: GroundingWithBingSearch
          target: https://api.bing.microsoft.com/
          authType: ApiKey
          credentials:
            key: ${PARAM_BING_GROUNDING_CONN_KEY}
      resources:
        - resource: bing_grounding
          connectionName: bing-grounding-conn
```

### Env vars

```bash
azd env set PARAM_BING_GROUNDING_CONN_KEY "<bing-search-resource-key>"
```

Simpler alternative -- if you just want plain web search without specific Bing grounding semantics, use the built-in `web_search` tool in a toolbox. It needs no connection.

```yaml
toolboxes:
  - name: misc-tools
    tools:
      - type: web_search
```

---

## Recipe: OpenAPI tool with API-key auth

User intent: "Wire up my internal REST API as a tool. It uses a static API key."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: contoso-api-conn
    category: ApiKey
    target: https://api.contoso.com
    authType: ApiKey
    credentials:
      key: "{{ contoso_api_key }}"
  - kind: toolbox
    name: agent-tools
    tools:
      - type: openapi
        project_connection_id: contoso-api-conn
        # The OpenAPI spec lives in your agent source and gets uploaded at deploy time.
```

### Resulting `azure.yaml`

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
```

### Env vars

```bash
azd env set PARAM_CONTOSO_API_CONN_KEY "<api-key>"
```

---

## Recipe: A2A (Agent-to-Agent) bridge

User intent: "Let my agent delegate work to another deployed agent."

### Manifest fragment

```yaml
resources:
  - kind: connection
    name: peer-agent-conn
    category: RemoteTool
    target: https://other-agent.foundry-account.westus2.azure.com/
    authType: ProjectManagedIdentity
    audience: https://ai.azure.com/.default
  - kind: toolbox
    name: agent-tools
    tools:
      - type: a2a_preview
        project_connection_id: peer-agent-conn
```

### Resulting `azure.yaml`

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

### Env vars

None. ProjectManagedIdentity means the agent's MI calls the peer agent; no static credential.

---

## Recipe: Multiple connections in one toolbox

Toolboxes can mix any number of tools (built-in + custom) referencing different connections.

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

Caveat (from Microsoft tool catalog docs): a toolbox supports at most ONE tool of a given built-in type without a `name:` field. To include multiple instances of the same type, set a unique `name` on each.

---

## Removing a connection

1. Remove the `connection` entry from `azure.yaml` `services.<name>.config.connections[]` (or `toolConnections[]`).
2. Remove any tools that reference it by `project_connection_id` from the corresponding toolbox `tools[]`, and any `resources[]` entry with matching `connectionName`.
3. `azd env unset PARAM_<...>` for the credential env vars (optional but tidy).
4. `azd provision` -- Bicep removes the connection from the Foundry project.
5. `azd deploy` -- updates the toolbox to drop the tool.

If the connection was created imperatively (via `azd ai agent connection create`), use `azd ai agent connection delete <name>` instead. `azd provision` will not touch it.
