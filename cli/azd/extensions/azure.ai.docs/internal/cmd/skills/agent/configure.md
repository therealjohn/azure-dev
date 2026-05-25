---
short: Shape the agent before deploying (model, instructions, tools, connections).
order: 20
---
# Configure: shape the agent before deploying

Audience: an AI coding assistant configuring a Foundry agent project that has already passed the `initialize` workflow (`azure.yaml` exists, has at least one ai.agent service). Every command below is idempotent or gated by the confirmation envelope -- safe to script.

---

## The agent manifest

Every ai.agent service has an `agent.yaml` at `<service-dir>/agent.yaml`. That file is the source of truth for the deployed agent: instruction, model, skills, tool list, endpoint/card configuration, environment variables.

`azd deploy` reads `agent.yaml` and creates a new agent version on the Foundry account. Re-deploys after editing the file create another version (versions are immutable).

For the FULL on-disk shape (every supported field, tool kind, connection category, parameters block, round-trip semantics), see the `extend` topic. This topic focuses on the operational surface around the manifest: connections, file uploads, endpoint patches, eval init, env vars.

---

## Connection management

Foundry projects can have multiple connections (search indexes, MCP servers, A2A endpoints, ACR, AI Services, Bing, etc). Connections are referenced by name from `agent.yaml`.

### List connections

```bash
azd ai agent connection list --output json
```

Returns an array of `{name, kind, authType, target}` objects.

### Inspect one connection

```bash
azd ai agent connection show <name> --output json
```

Returns the full record including credentials when allowed.

### Create / update / delete connections

`connection create`, `connection update`, and `connection delete` live in a separate package and do NOT yet integrate with the structured confirmation envelope (the envelope's `confirmation_required` JSON shape used elsewhere). They have a simpler `--force` flag for non-interactive use.

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

Flag reference:

- `--kind <kind>` -- connection kind: `remote-tool`, `remote-a2a`, `cognitive-search`, etc. Free-form -- matches the `category:` values used in `agent.yaml` (lowercased / hyphenated). See `extend` for the full category list.
- `--target <url>` -- target endpoint URL or ARM resource ID.
- `--auth-type <type>` -- one of `api-key`, `custom-keys`, `none`, `oauth2`, `user-entra-token`, `project-managed-identity`, `agentic-identity`.
- `--key <value>` -- API key (for `api-key` auth).
- `--custom-key key=value` (repeatable) -- multiple custom key/value pairs (for `custom-keys` auth).
- `--client-id <id>` / `--client-secret <secret>` -- OAuth2 client credentials (required for `oauth2` auth).
- `--audience <value>` -- token audience for `user-entra-token` and `agentic-identity` auth.
- `--metadata key=value` (repeatable) -- additional metadata sent to the Foundry connection record.
- `--force` -- on `create`, upsert if the connection already exists; on `delete`, skip the y/n prompt in non-interactive mode.

Cross-link: to declare connections that should be CREATED at `azd provision` time (via Bicep), put them in `agent.yaml`'s `resources:` block. See the `extend` topic for that shape.

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

When the only thing that changed in `agent.yaml` is the `agent_endpoint` or `agent_card` block, you don't need a full redeploy. The `endpoint update` command patches those fields in place WITHOUT creating a new agent version:

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
