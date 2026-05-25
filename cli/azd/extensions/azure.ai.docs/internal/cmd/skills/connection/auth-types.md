---
short: Reference of auth types, credential shapes, and PARAM_* externalization.
order: 40
---
# Connection auth types

Reference for the `authType:` field on a connection. Picks WHAT kind of credential the Foundry runtime injects at tool-call time. The `credentials:` map's shape depends entirely on this value.

For end-to-end scenario examples, see `add`. For category selection, see `categories`.

---

## Auth-type table

| `authType:` (manifest / azure.yaml) | CLI flag (`--auth-type`) | Required credential shape                                                                   | Common with                                                  |
| ----------------------------------- | ------------------------ | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `ApiKey`                            | `api-key`                | `{ key: <string> }`                                                                         | `ApiKey`, `CognitiveSearch`, `GroundingWithBingSearch`, `ContainerRegistry` |
| `CustomKeys`                        | `custom-keys`            | `{ keys: { <header>: <string>, ... } }`                                                     | `RemoteTool`, `CustomKeys` (MCP with multiple headers)       |
| `OAuth2`                            | (use flag)               | `{ clientId, clientSecret }` (also accepts `authUrl`, `tokenUrl`, `refreshUrl`, `scopes`, `tenantId`, `username`, `password`, `developerToken`, `refreshToken`) | `RemoteTool` (OAuth-protected MCP / OpenAPI)                 |
| `UserEntraToken`                    | `user-entra-token`       | none -- token sourced from the END USER's Entra session at call time. `audience:` required at the connection level. | `RemoteTool` (1P MCP server that needs OBO)                  |
| `AgenticIdentity`                   | `agentic-identity`       | none -- token sourced from the AGENT's identity. `audience:` required at the connection level. | `RemoteTool` (downstream service trusting the agent's MI)    |
| `ProjectManagedIdentity`            | `project-managed-identity` | none -- token sourced from the Foundry project's MI. Optional `audience:`.                 | `RemoteTool`, A2A (downstream service trusting the project MI) |
| `PAT`                               | (no slug)                | `{ pat: <string> }`                                                                         | `Git`                                                        |
| `AAD`                               | (no slug)                | none -- AAD principal resolved from the caller. Optional `audience:`.                       | `CognitiveSearch`, `AzureOpenAI`, `AzureBlob` (use the agent's MI to access via AAD) |
| `ServicePrincipal`                  | (no slug)                | `{ clientId, clientSecret, tenantId }`                                                      | `RemoteTool`, any downstream Azure service                   |
| `UsernamePassword`                  | (no slug)                | `{ username, password }`                                                                    | `Redis`, `Snowflake`, `AzureSqlDb`                           |
| `AccessKey` / `AccountKey`          | (no slug)                | `{ key: <string> }`                                                                         | `AzureBlob`, `ADLSGen2`                                      |
| `SAS`                               | (no slug)                | `{ sas: <string> }`                                                                         | `AzureBlob`                                                  |
| `None`                              | `none`                   | omit `credentials:` entirely                                                                | Anonymous endpoints (public MCP, public OpenAPI)             |

CLI slugs that don't appear above (e.g. for `PAT`, `ServicePrincipal`) require passing the ARM-canonical form via the imperative create command, or hand-editing `azure.yaml`.

---

## `credentials.type:` promotion

Some sample manifests put the auth type INSIDE the credentials map (`credentials.type: CustomKeys`) instead of at the top level (`authType: CustomKeys`). Init promotes this before writing to `azure.yaml`:

```yaml
# Manifest (either form works)
- kind: connection
  name: my-conn
  credentials:
    type: CustomKeys
    keys:
      Authorization: "Bearer {{ pat }}"

# OR
- kind: connection
  name: my-conn
  authType: CustomKeys
  credentials:
    keys:
      Authorization: "Bearer {{ pat }}"
```

Both produce the same `azure.yaml` output. If both are set, `authType:` wins. The `type:` key is removed from `credentials:` during the promotion so it doesn't get externalized as a fake `PARAM_*` env var.

When you write `azure.yaml` directly, ALWAYS put `authType:` at the top level. The `credentials.type:` shorthand is an init-time convenience only.

---

## Credential externalization (`PARAM_*` env vars)

EVERY string leaf in a connection's `credentials:` map is externalized at init time. This is not optional. The rule:

1. For each `credentials.<key>: "<value>"` (or `credentials.keys.<header>: "<value>"`, etc.), init computes an env var name: `PARAM_<UPPER_CONN_NAME>_<UPPER_KEY_PATH>`, with non-alphanumeric characters replaced by `_`.
2. The raw value is stored via `azd env set PARAM_<...> <value>` in the active environment.
3. The value in `azure.yaml` is replaced with `${PARAM_<...>}`.

Examples:

| Manifest input                                                         | azd env var set                              | `azure.yaml` value                           |
| ---------------------------------------------------------------------- | -------------------------------------------- | -------------------------------------------- |
| `credentials.key: "abc123"` on connection `my-search`                   | `PARAM_MY_SEARCH_KEY=abc123`                 | `key: ${PARAM_MY_SEARCH_KEY}`                |
| `credentials.keys.Authorization: "Bearer xyz"` on `github-mcp-conn`     | `PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION=Bearer xyz` | `Authorization: ${PARAM_GITHUB_MCP_CONN_KEYS_AUTHORIZATION}` |
| `credentials.clientId: "id"` + `credentials.clientSecret: "sec"` on `my-oauth` | `PARAM_MY_OAUTH_CLIENTID=id` + `PARAM_MY_OAUTH_CLIENTSECRET=sec` | `clientId: ${PARAM_MY_OAUTH_CLIENTID}` + `clientSecret: ${PARAM_MY_OAUTH_CLIENTSECRET}` |

Nested maps preserve structure (the env var name accumulates the path):

```yaml
credentials:
  keys:
    Authorization: "Bearer ..."      # -> PARAM_<CONN>_KEYS_AUTHORIZATION
    X-Tenant: "contoso"              # -> PARAM_<CONN>_KEYS_X_TENANT
```

The hyphen in `X-Tenant` collapses to `_`, so `PARAM_<CONN>_KEYS_X_TENANT`.

Why externalize: `azure.yaml` is checked into source control; raw secrets cannot be. The `.env` file under the azd environment IS gitignored by default; that's the right place for the actual values.

### Setting credentials manually

When you add a connection to `azure.yaml` post-init, do the externalization yourself:

```bash
# 1. Write azure.yaml with ${PARAM_MY_NEW_CONN_KEY} placeholder
# 2. Set the raw value
azd env set PARAM_MY_NEW_CONN_KEY "<the-actual-secret>"
# 3. Run azd provision
```

`azd env set` writes to `<env>/.env`. Use `azd env list` to see what's defined.

---

## When NO credentials are needed

`UserEntraToken`, `AgenticIdentity`, `ProjectManagedIdentity`, `AAD`, `None`: omit `credentials:` entirely. The first three require `audience:` at the connection level instead.

```yaml
# UserEntraToken (1P OBO)
- name: workiq-mail-conn
  category: RemoteTool
  target: https://agent365.svc.cloud.microsoft/agents/servers/mcp_MailTools
  authType: UserEntraToken
  audience: ea9ffc3e-8a23-4a7d-836d-234d7c7565c1

# ProjectManagedIdentity (downstream Azure service)
- name: peer-agent-conn
  category: RemoteTool
  target: https://other-agent.foundry.azure.com/
  authType: ProjectManagedIdentity
  audience: https://ai.azure.com/.default

# None (public MCP)
- name: public-mcp-conn
  category: RemoteTool
  target: https://example.com/mcp
  authType: None
```

For `UserEntraToken` and `AgenticIdentity`, the Foundry project needs the right Entra app registration / role assignment for the OBO or agent-identity flow to succeed. This is usually a one-time setup outside the agent extension's scope.

---

## OAuth2 details

OAuth2 is the trickiest auth type because the credential shape depends on the flow.

### Client-credentials flow (your own OAuth app)

```yaml
- name: my-oauth-conn
  category: RemoteTool
  target: https://api.example.com/mcp
  authType: OAuth2
  credentials:
    clientId: ${PARAM_MY_OAUTH_CONN_CLIENTID}
    clientSecret: ${PARAM_MY_OAUTH_CONN_CLIENTSECRET}
  authorizationUrl: https://login.example.com/oauth/authorize
  tokenUrl: https://login.example.com/oauth/token
  refreshUrl: https://login.example.com/oauth/refresh
  scopes: [read, write]
```

CLI form:

```bash
azd ai agent connection create my-oauth-conn \
  --kind remote-tool \
  --target https://api.example.com/mcp \
  --auth-type oauth2 \
  --client-id "<id>" \
  --client-secret "<secret>"
```

### Foundry-managed OAuth (Microsoft hosts the OAuth app)

Microsoft maintains pre-registered OAuth apps for common providers. Reference one by `connectorName:`:

```yaml
- name: github-oauth-conn
  category: RemoteTool
  target: https://api.githubcopilot.com/mcp
  authType: OAuth2
  connectorName: foundrygithubmcp
  credentials:
    type: OAuth2     # required, but no clientId/clientSecret needed
```

End users complete the OAuth handshake on first call; no client credentials needed.

---

## Validating after edits

```bash
azd ai agent doctor --output json
```

Look for `remote.connections` (and `local.agent-yaml-valid` for the agent.yaml itself). The check fails when:

* `authType:` is set to a value the parser doesn't recognize.
* `credentials:` is missing fields required by `authType:`.
* `audience:` is missing for `UserEntraToken` / `AgenticIdentity`.
* A `${PARAM_*}` reference points at an env var that isn't set.
