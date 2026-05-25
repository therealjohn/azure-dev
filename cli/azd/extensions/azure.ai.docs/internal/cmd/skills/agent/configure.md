---
short: Shape the agent before deploying (service config, model, connections, endpoint, evals).
order: 20
---
# Configure: shape the agent before deploying

Audience: an AI coding assistant configuring a Foundry agent project that has already passed the `initialize` workflow (`azure.yaml` exists, has at least one ai.agent service). Every command below is idempotent or gated by the confirmation envelope -- safe to script.

---

## The two files (read this first)

Every ai.agent service is defined by TWO files that together drive deploy:

* `<service-dir>/agent.yaml` -- the FLAT `ContainerAgent` (kind, name, protocols, environment_variables, agentEndpoint, agentCard, codeConfiguration, image, container cpu/memory). The shape and editing rules live in the `extend` topic.
* `azure.yaml` under `services.<name>.config` -- the service configuration (model deployments, connections, toolboxes, tool resources, container settings, startup command). THIS topic is the reference for that block.

The two files load through different code paths at deploy time. Putting a field in the wrong file is the most common cause of "the change had no effect."

`azd deploy` reads `agent.yaml` and creates a new immutable agent version on the Foundry account. `azd provision` reads `azure.yaml`'s config block (specifically `deployments[]` and `connections[]`) and applies them through the Bicep pipeline.

---

## Service config (`azure.yaml services.<name>.config`)

The full schema, drawn from `ServiceTargetAgentConfig`. Every field is optional unless noted.

```yaml
# azure.yaml
services:
  my-agent:
    project: ./src/my-agent
    host: ai.agent
    language: docker            # or "python" / "csharp" for code deploy
    docker:
      remoteBuild: true         # absent for code deploy
    config:
      startupCommand: "python -m main"   # used by `azd ai agent run` (local)
      container:
        resources:
          cpu: "0.5"
          memory: "1Gi"
      deployments:
        - name: AZURE_AI_MODEL_DEPLOYMENT_NAME   # azd env var name the deployed agent reads
          model:
            name: gpt-4.1-mini
            format: OpenAI
            version: "2024-04-09"
          sku:
            name: GlobalStandard
            capacity: 50
      connections:
        - name: github-mcp-conn
          category: RemoteTool
          target: https://api.githubcopilot.com/mcp
          authType: CustomKeys
          credentials:
            Authorization: ${PARAM_GITHUB_MCP_CONN_AUTHORIZATION}
      toolboxes:
        - name: agent-tools
          description: "MCP toolset bundling GitHub + web search."
          tools:
            - type: web_search
            - type: mcp
              server_label: github
              project_connection_id: github-mcp-conn
      toolConnections:
        # Auto-extracted from toolbox tools that had `target:` + `authType:` in the manifest.
        # Equivalent to `connections[]` in shape -- the split exists for historical reasons.
        - name: extra-mcp
          category: RemoteTool
          target: https://example.com/mcp
          authType: ApiKey
          credentials:
            key: ${PARAM_EXTRA_MCP_KEY}
      resources:
        # Built-in tools that require a named connection (bing_grounding, azure_ai_search).
        # The connection must exist on the Foundry project (either declared above or pre-existing).
        - resource: azure_ai_search
          connectionName: my-search-conn
```

Field-by-field reference:

* `startupCommand` -- string. Command `azd ai agent run` uses to spawn the agent locally. Auto-detected at init from the agent's source layout.
* `container.resources.{cpu,memory}` -- container resource tier. Mirrors the values offered by init: `0.5/1Gi`, `1/2Gi`, `2/4Gi`. These are the CONTAINER settings (where the agent process runs); the per-agent-version `resources:` block inside `agent.yaml` is a separate concept (Foundry uses its own internal scheduling for that).
* `deployments[]` -- model deployments to provision at `azd provision` time via the Bicep pipeline. `name` is the azd env var the deployed `agent.yaml` references (typically `AZURE_AI_MODEL_DEPLOYMENT_NAME`); `model.name/format/version` is the Foundry catalog entry; `sku.name/capacity` controls cost and throughput.
* `connections[]` -- Foundry project connections to create at `azd provision`. Full schema below.
* `toolboxes[]` -- reusable tool bundles published to the Foundry project at `azd deploy` time. Per Microsoft's tool-catalog docs: "a toolbox is a curated bundle of tools that you configure once and expose as a single MCP-compatible endpoint." Each entry has `name`, optional `description`, and a `tools[]` array of tool objects. See "Toolbox shape" below.
* `toolConnections[]` -- same shape as `connections[]`. Init hoists these out of toolbox `tools[]` entries that had `target:` + `authType:` in the seed manifest; deploy treats them identically to `connections[]`. You can put a new entry in either array -- pick `connections[]` for new manual edits.
* `resources[]` -- built-in tool resources that need a pre-existing connection name. ONLY `bing_grounding` and `azure_ai_search` are recognized here today; everything else lives in a toolbox.

### Connection shape (used by `connections[]` and `toolConnections[]`)

```yaml
- name: <connection-name>            # required; what tools reference as project_connection_id
  category: <CategoryKind>           # required; ARM-canonical (RemoteTool, CognitiveSearch, ApiKey, OAuth2, ...). See `azd ai doc connection categories`.
  target: <url-or-arm-id>            # required for most categories
  authType: <AuthType>               # required; ApiKey | CustomKeys | OAuth2 | UserEntraToken | AgenticIdentity | ProjectManagedIdentity | None | ...
  credentials:                       # shape depends on authType; see `azd ai doc connection auth-types`
    <key>: ${PARAM_<CONN>_<KEY>}     # values referenced as env vars, NEVER raw secrets
  metadata:
    <key>: <value>                   # category-specific (e.g. indexName for CognitiveSearch)
  audience: <token-audience>         # required for UserEntraToken, AgenticIdentity, some ProjectManagedIdentity
  authorizationUrl: ...              # OAuth2 only
  tokenUrl: ...
  refreshUrl: ...
  scopes: [...]
  connectorName: ...                 # OAuth2 only; for Microsoft-managed OAuth apps (e.g. foundrygithubmcp)
  isSharedToAll: true
  sharedUserList: [...]
```

For the full deep-dive on every category, every auth type, the credential shape each one expects, and how to set the `${PARAM_...}` env vars referenced above, see `azd ai doc connection <topic>`.

### Toolbox shape

```yaml
toolboxes:
  - name: my-toolbox
    description: ...
    tools:
      # Built-in tools (no connection): just `type` and an optional `name`.
      - type: web_search
      - type: code_interpreter
      - type: file_search
      # Custom tools (external endpoint): `type` + `project_connection_id` pointing
      # at a connection in connections[] / toolConnections[]. Optional server_label,
      # server_url, etc. depending on tool type.
      - type: mcp
        server_label: github
        project_connection_id: github-mcp-conn
      - type: openapi
        project_connection_id: my-api-conn
      - type: a2a_preview
        project_connection_id: my-agent-conn
```

Tool taxonomy (matches the official Foundry tool catalog):

| Category    | Tool types                                                              | Connection required?                                |
| ----------- | ----------------------------------------------------------------------- | --------------------------------------------------- |
| Built-in    | `web_search`, `code_interpreter`, `file_search`, `function`             | No                                                  |
| Built-in (connection-bound) | `bing_grounding`, `azure_ai_search`                     | Yes -- via `resources[]` (outside the toolbox) OR via toolbox tool with `project_connection_id`. |
| Custom      | `mcp`, `openapi`, `a2a_preview`                                         | Yes -- `project_connection_id` on the tool entry.   |

`mcp` here is the AgentSchema tool kind -- it lets your AGENT CALL OUT to an MCP server. It is unrelated to `azd ai agent mcp start`, which exposes the azd ai agent CLI itself to IDEs over MCP. The two are not interchangeable.

---

## Manifest -> azure.yaml: the init transform contract

`azd ai agent init -m <manifest-url>` reads an `agent.manifest.yaml` (with `template:` wrapper + outer `parameters:` + outer `resources[]`) and SPLITS it across the on-disk files. The same split is what you mimic manually when adding resources post-init. Quick reference:

| Manifest fragment                         | Lands in `azure.yaml services.<name>.config`                     |
| ----------------------------------------- | ---------------------------------------------------------------- |
| `template.environment_variables[]`        | `<service>/agent.yaml` `environment_variables[]` (NOT in azure.yaml) |
| `resources[]` of `kind: model`            | `deployments[]` (one per entry; `name` becomes the env var the agent.yaml binds to) |
| `resources[]` of `kind: tool, id: bing_grounding` / `id: azure_ai_search` | `resources[]` `{resource, connectionName}` (init PROMPTS for the connection name) |
| `resources[]` of `kind: toolbox`          | `toolboxes[]`. External tools (with `target:` + `authType:`) get hoisted into `toolConnections[]` and the tool entry is rewritten to reference them by `project_connection_id`. |
| `resources[]` of `kind: connection`       | `connections[]`                                                  |
| Any connection `credentials.<key>: <value>` (string leaf) | `${PARAM_<CONN>_<KEY>}` in azure.yaml; raw value stored via `azd env set PARAM_<CONN>_<KEY> <value>`. Nested maps preserve structure; only string leaves get externalized. |
| Connection with `credentials.type: <X>` but no top-level `authType:` | `authType: <X>` promoted to top-level before externalization. |

If you're adding a tool / connection AFTER init, do the equivalent: edit `azure.yaml`'s config block directly, then `azd env set PARAM_<...>` for any credential value, then `azd provision && azd deploy`. See `azd ai doc connection add` for end-to-end recipes.

---

## Connection management

Foundry projects can have multiple connections (search indexes, MCP servers, A2A endpoints, ACR, AI Services, Bing, etc.). Connections are referenced by `project_connection_id` from `azure.yaml`'s toolbox `tools[]` entries and from `resources[]` `connectionName` fields.

THREE ways a connection comes to exist:

1. **Declared in `azure.yaml`** -- `services.<name>.config.connections[]` (or `toolConnections[]`). Created at `azd provision` via Bicep. Recommended for project-owned, environment-specific connections.
2. **Pre-existing on the Foundry project** -- created out-of-band (portal, another team's deploy). Referenced by name only; not in `azure.yaml`.
3. **Imperative via the CLI** -- `azd ai agent connection create/update/delete`. Lives only on the Foundry project, never in any local file. Useful for one-off shared dev connections.

For the full mental model + when to use which path, see `azd ai doc connection overview`. For end-to-end recipes, see `azd ai doc connection add`.

### Inspect connections

```bash
azd ai agent connection list --output json     # name, kind, authType, target
azd ai agent connection show <name> --output json   # full record (incl. credentials when allowed)
```

### Imperative create / update / delete

The `azd ai agent connection create/update/delete` commands target the Foundry project directly and do NOT touch `azure.yaml`. They have a simpler `--force` flag for non-interactive use (no `confirmation_required` envelope).

```bash
# Create (or upsert with --force when the connection already exists)
azd ai agent connection create my-search \
  --kind cognitive-search \
  --target https://my-search.search.windows.net/ \
  --auth-type api-key \
  --key "<key>"

# OAuth2 connection
azd ai agent connection create my-mcp \
  --kind remote-tool \
  --target https://api.example.com/mcp \
  --auth-type oauth2 \
  --client-id "<id>" \
  --client-secret "<secret>"

# Custom-keys auth (multiple key=value pairs)
azd ai agent connection create my-svc \
  --kind remote-tool \
  --target https://api.example.com \
  --auth-type custom-keys \
  --custom-key apiKey=abc \
  --custom-key region=eastus \
  --metadata environment=prod

azd ai agent connection update my-search --target https://my-search-2.search.windows.net/
azd ai agent connection delete my-search --force
```

For the full flag reference, slug -> ARM-canonical mappings, and the auth-type -> credential-shape lookup, see `azd ai doc connection manage`.

When running in agent mode, prefer the explicit per-flag form -- the connection commands do not yet emit `confirmation_required` envelopes, so the human's consent is gathered out-of-band (you ask, they reply).

---

## File uploads

Upload local files into a session's filesystem (e.g. data assets the agent will reference at runtime):

```bash
azd ai agent files upload ./data/input.csv
azd ai agent files upload ./input.csv --target-path /data/input.csv
azd ai agent files list --output json
```

Delete is gated by the confirmation envelope (see `operate`).

---

## Endpoint and card configuration

When the only thing that changed in `agent.yaml` is the `agentEndpoint` or `agentCard` block, you don't need a full redeploy. The `endpoint update` command patches those fields in place WITHOUT creating a new agent version:

```bash
# Preview
azd ai agent endpoint update --dry-run

# Apply after reviewing the dry-run envelope
azd ai agent endpoint update --force
```

Exit `2` + JSON envelope means the agent is non-interactive and needs explicit `--force` to proceed. Present the envelope's `changes` to the human and re-run the `confirmCommand`.

---

## Eval configuration

The eval `init` command lives here because it SHAPES the eval suite (generates `eval.yaml`, dataset, evaluator definitions). The end-to-end eval lifecycle -- init -> run -> show -> update -> re-run -- lives in the `evaluate` topic.

```bash
# Generate eval.yaml + dataset + evaluator (preview first)
azd ai agent eval init --dry-run

# Apply
azd ai agent eval init --force

# Inspect what got generated
azd ai agent eval show --output json
azd ai agent eval list --output json
```

`eval init` runs billed generation jobs and is gated by the confirmation envelope. See `evaluate` for the full flag set and `operate` for `eval run` (the billed execution step).

---

## Where state lives

Configuration values used by the agent extension are read from the active azd environment unless overridden by a flag or shell variable.

| Variable                       | Read by                       |
| ------------------------------ | ----------------------------- |
| `AZURE_AI_PROJECT_ENDPOINT`    | Every command that resolves the project endpoint cascade. |
| `FOUNDRY_PROJECT_ENDPOINT`     | Host-shell fallback when no azd env value. |
| `AZURE_AI_PROJECT_ID`          | `show` for the playground URL. |
| `AGENT_<SVC>_<PROTO>_ENDPOINT` | `show` / `invoke` for per-protocol deployed endpoints. |
| `AGENT_<SVC>_ENDPOINT`         | Legacy single-endpoint fallback. |
| `PARAM_<CONN>_<KEY>`           | Connection credential values referenced from `azure.yaml`'s `connections[]` / `toolConnections[]`. Set with `azd env set`. |
| `AI_AGENT_PENDING_PROVISION`   | Internal next-step resolution. |

Manage them via `azd env get|set|list|new|select`.

---

## Common error codes

- `invalid_agent_manifest` -- `agent.yaml` is malformed. Validate with `azd ai agent doctor --output json` (look for the `agent-yaml-valid` check).
- `invalid_connection` -- a connection refused by the Foundry service. Inspect with `azd ai agent connection show <name> --output json`.
- `missing_connection_field` -- `connection update` needs at least `--target`, `--key`, or `--custom-key`.
- `invalid_agent_request` -- the patch produced by `endpoint update` was rejected. Re-read `agent.yaml` to confirm the change you intended is expressible.

---

## Confirmation envelope reminder

Every write command in this topic accepts `--dry-run` and `--force`. `--dry-run` prints the envelope and exits 0; agent-mode without `--force` prints the envelope and exits 2. See the README's "Confirmation envelope for write commands" section for the JSON shape.
