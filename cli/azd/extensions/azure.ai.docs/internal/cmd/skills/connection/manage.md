---
short: Imperative CLI for connections (list, show, create, update, delete).
order: 50
---
# Connection management (imperative CLI)

Reference for `azd ai agent connection` -- the imperative path for connection lifecycle. These commands target the Foundry project directly and do NOT touch `azure.yaml`.

For the declarative path (connections defined in `azure.yaml` and provisioned via Bicep), see `overview` and `add`. The two paths coexist; this topic covers when to reach for the CLI instead of editing `azure.yaml`.

> **Note:** The connection commands currently live under `azd ai agent connection ...`. A separate `azd ai connection ...` namespace exists as a stub in the `azure.ai.connections` extension; that's the eventual destination once the namespace move lands. Use `azd ai agent connection ...` for now.

---

## When to use the imperative path

Reach for `azd ai agent connection create` when:

* You're experimenting and don't want the connection re-created on every `azd provision`.
* The connection is shared across teams / projects and owned by someone other than the azd project (e.g. a centrally-managed AI Search service the dev team uses).
* You're adding a connection AFTER an `azd provision` has already run, and you don't want to re-run provision just for one connection.
* You're scripting a one-off ops task (rotate a key, change a target URL).

Reach for the declarative path (edit `azure.yaml`) when:

* The connection is project-owned and should be reproducible on every fresh provision.
* You want secrets stored as `PARAM_*` env vars under source control as `${PARAM_*}` references.
* You want the connection to be deleted when the project is `azd down`'d.

---

## List

```bash
azd ai agent connection list --output json
azd ai agent connection list --kind cognitive-search --output json
```

Returns an array of `{name, kind, authType, target}` objects. `--kind` filters server-side. Accepts both kebab-case slugs and ARM-canonical PascalCase (see `categories`).

---

## Show

```bash
azd ai agent connection show <name> --output json
azd ai agent connection show <name> --show-credentials --output json
```

`--show-credentials` fetches the data-plane response that includes the raw secret values. Use this to recover an API key you don't have stored locally (the response is logged-only-to-stdout, never persisted by the CLI).

---

## Create

```bash
azd ai agent connection create <name> \
  --kind <category> \
  --target <url-or-arm-id> \
  --auth-type <auth-type> \
  [auth-specific flags]
```

### Per-auth-type quick reference

```bash
# ApiKey
azd ai agent connection create my-search \
  --kind cognitive-search \
  --target https://my-search.search.windows.net/ \
  --auth-type api-key \
  --key "<key>"

# CustomKeys (multiple key=value, repeatable)
azd ai agent connection create my-mcp \
  --kind remote-tool \
  --target https://api.example.com/mcp \
  --auth-type custom-keys \
  --custom-key Authorization="Bearer xyz" \
  --custom-key X-Tenant=contoso

# OAuth2
azd ai agent connection create my-oauth-mcp \
  --kind remote-tool \
  --target https://api.example.com/mcp \
  --auth-type oauth2 \
  --client-id "<id>" \
  --client-secret "<secret>"

# UserEntraToken (no key; audience required)
azd ai agent connection create workiq-mail \
  --kind remote-tool \
  --target https://agent365.svc.cloud.microsoft/agents/servers/mcp_MailTools \
  --auth-type user-entra-token \
  --audience ea9ffc3e-8a23-4a7d-836d-234d7c7565c1

# AgenticIdentity (no key; audience required)
azd ai agent connection create downstream-svc \
  --kind remote-tool \
  --target https://internal.contoso.com/api \
  --auth-type agentic-identity \
  --audience https://contoso.com/.default

# ProjectManagedIdentity (no key; audience optional)
azd ai agent connection create peer-agent \
  --kind remote-tool \
  --target https://other-agent.foundry.azure.com/ \
  --auth-type project-managed-identity

# None (anonymous)
azd ai agent connection create public-mcp \
  --kind remote-tool \
  --target https://example.com/mcp \
  --auth-type none

# With metadata (repeatable)
azd ai agent connection create my-search \
  --kind cognitive-search \
  --target https://my-search.search.windows.net/ \
  --auth-type api-key --key "<key>" \
  --metadata indexName=docs-corpus \
  --metadata environment=prod
```

### Replace an existing connection

```bash
# Upsert (replaces if exists, creates if not)
azd ai agent connection create my-search \
  --kind cognitive-search \
  --target https://my-search.search.windows.net/ \
  --auth-type api-key \
  --key "<new-key>" \
  --force
```

Without `--force`, an existing connection causes `connection_already_exists`.

### All flags

| Flag                  | Required when                                | What it does                                                      |
| --------------------- | -------------------------------------------- | ----------------------------------------------------------------- |
| `--kind <category>`   | always                                       | Connection category. Slug or ARM-canonical. See `categories`.     |
| `--target <url>`      | always                                       | Endpoint URL or ARM resource ID.                                  |
| `--auth-type <type>`  | always (defaults to `none`)                  | One of `api-key`, `custom-keys`, `none`, `oauth2`, `user-entra-token`, `project-managed-identity`, `agentic-identity`. |
| `--key <value>`       | `--auth-type api-key`                        | API key value.                                                    |
| `--custom-key k=v`    | `--auth-type custom-keys`                    | Repeatable. Each becomes a header (or per-category-specific KV).  |
| `--client-id <id>`    | `--auth-type oauth2`                         | OAuth2 client ID.                                                 |
| `--client-secret <s>` | `--auth-type oauth2`                         | OAuth2 client secret.                                             |
| `--audience <value>`  | `--auth-type user-entra-token \| agentic-identity` | Token audience (the downstream resource's app ID URI or `https://<host>/.default`). |
| `--metadata k=v`      | optional                                     | Repeatable. Category-specific metadata (e.g. `indexName=...` for CognitiveSearch). |
| `--force`             | optional                                     | Upsert on `create`; skip the y/n prompt on `delete`.              |
| `-p, --project-endpoint <url>` | optional                            | Override the Foundry project endpoint. Falls back to `AZURE_AI_PROJECT_ENDPOINT` then to azd config. |
| `-o, --output table\|json` | optional                                 | Defaults to `table`.                                              |

---

## Update

Partial update -- only changes the fields specified. Refetches existing credentials and merges to avoid clobbering.

```bash
# Change target only
azd ai agent connection update my-search --target https://my-search-2.search.windows.net/

# Rotate the API key only
azd ai agent connection update my-search --key "<new-key>"

# Update a custom-keys connection (keeps other keys; updates / adds named one)
azd ai agent connection update my-mcp --custom-key Authorization="Bearer new-token"
```

Requires at least one of `--target`, `--key`, or `--custom-key`; otherwise `missing_connection_field`.

Update intentionally CANNOT change `--kind` or `--auth-type` -- delete and re-create for those.

---

## Delete

```bash
# Interactive (prompts y/n)
azd ai agent connection delete my-search

# Non-interactive
azd ai agent connection delete my-search --force
```

Delete removes the connection from the Foundry project. Any tool that referenced it by `project_connection_id` will fail at call time until the reference is removed or repointed. Audit with `azd ai agent doctor --output json` afterwards.

If the connection was DECLARED in `azure.yaml`, the next `azd provision` recreates it. To permanently remove, delete the entry from `azure.yaml` first, then run delete.

---

## Common error codes

* `connection_already_exists` -- `create` without `--force` against an existing name.
* `missing_connection_field` -- `update` without any of `--target`, `--key`, `--custom-key`. Or `create` missing `--kind` / `--target` / `--key` (for api-key) / `--custom-key` (for custom-keys) / `--client-id` + `--client-secret` (for oauth2).
* `conflicting_arguments` -- e.g. `--audience` set with an auth type that doesn't take it, or `--client-id` set without `--auth-type oauth2`.
* `invalid_connection` -- ARM rejected the connection (target unreachable, credentials malformed, category not supported by the Foundry project's tier).

---

## Confirmation envelope status

The connection CLI does NOT yet emit `confirmation_required` JSON envelopes (the structured exit-2 contract used by other write commands like `deploy`, `endpoint update`, `files delete`). They have a simpler `--force` flag for non-interactive use.

When running in agent mode, prefer the explicit per-flag form -- the human's consent is gathered out-of-band (you ask, they reply), then you run with `--force` if needed.
