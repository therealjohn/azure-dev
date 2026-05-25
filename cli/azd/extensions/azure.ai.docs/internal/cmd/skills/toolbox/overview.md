---
short: What a toolbox is, how azd handles it, and the deploy lifecycle.
order: 10
---
# Toolbox overview

A **toolbox** is a curated bundle of tools that Foundry exposes as a single MCP-compatible endpoint. The agent connects to one URL and dynamically discovers every tool inside. Toolboxes are the recommended way to attach multiple tools to an agent -- one endpoint, central credential handling, no per-agent tool wiring.

Per the Foundry tool catalog: built-in tools (`web_search`, `code_interpreter`, `file_search`), MCP servers, OpenAPI APIs, Azure AI Search, Bing Custom Search, A2A peer agents, and the `toolbox_search_preview` directive can all live in the same toolbox.

For step-by-step recipes, see `add`. For the full tool-type reference, see `tools`. For how agent code uses the endpoint, see `consume`.

## How azd handles toolboxes

Toolboxes are **declarative**. You define one under `services.<name>.config.toolboxes[]` in `azure.yaml`; azd takes over from there.

| Stage              | What happens                                                                                          |
| ------------------ | ----------------------------------------------------------------------------------------------------- |
| `azd ai agent init`| When the seed manifest has `kind: toolbox`, init writes it into `azure.yaml` and adds a matching `TOOLBOX_<NAME>_MCP_ENDPOINT` env var reference to the on-disk `agent.yaml`. |
| `azd provision`    | Bicep creates / updates any connections under `connections[]` / `toolConnections[]` referenced by the toolbox's tools. |
| `azd deploy`       | The agents extension calls `POST /toolboxes/{name}/versions?api-version=v1` for every toolbox in the service config. **A new version is created on every deploy** -- the toolbox itself is auto-created if it didn't exist. |
| Post-deploy        | The version-pinned MCP URL is written back to the azd environment as `TOOLBOX_<NAME>_MCP_ENDPOINT`. The deployed agent's `environment_variables` already references `${TOOLBOX_<NAME>_MCP_ENDPOINT}`, so the running agent gets the new URL. |

Two consequences worth knowing:

* **You don't manage toolbox creation or versions through azd's CLI.** There's no `azd ai agent toolbox` command. The lifecycle is implicit in deploy.
* **Every `azd deploy` produces a new version** of every toolbox in the project. Old versions stay on Foundry until you delete them via the SDK / REST / Foundry Toolkit. Agents deployed by azd always run against the latest version (it's the one in the env var).

To list, get, promote, or delete toolbox versions out-of-band, use the Python / .NET / JavaScript SDKs or the REST API directly. azd is intentionally create-only.

## Platform-injected env vars

When a hosted agent runs on Foundry (after `azd up`), the platform injects two env vars for every toolbox in the service config:

| Env var                              | What it is                                                                                |
| ------------------------------------ | ----------------------------------------------------------------------------------------- |
| `FOUNDRY_AGENT_TOOLBOX_ENDPOINT`     | Base URL for toolbox endpoints on this project (no toolbox name). Useful when you want to construct per-toolbox URLs from `TOOLBOX_NAME` at runtime. |
| `TOOLBOX_<NAME>_MCP_ENDPOINT`        | Full per-toolbox version-pinned MCP URL. Same name azd uses for the local-run env var.    |

For local runs (`azd ai agent run`), only `TOOLBOX_<NAME>_MCP_ENDPOINT` is set -- azd writes it to the active environment during `azd deploy`. The `FOUNDRY_AGENT_TOOLBOX_*` base URL is hosted-only.

Naming: the toolbox name is uppercased and non-alphanumeric characters collapse to `_`. `agent-tools` -> `TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT`. See `consume` for the full table.

## Developer vs consumer endpoint

Foundry exposes two endpoint patterns:

| Endpoint                                                                  | When                                                       |
| ------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `{project}/toolboxes/{name}/versions/{version}/mcp?api-version=v1`        | Version-pinned (developer / version-specific).             |
| `{project}/toolboxes/{name}/mcp?api-version=v1`                           | Default version (consumer). Always serves `default_version`.|

azd-deployed agents use the **version-pinned** form so deploys are reproducible -- the env var `TOOLBOX_<NAME>_MCP_ENDPOINT` always holds the version-specific URL of the version azd just published.

If you want consumer-endpoint behavior (auto-pickup of a new default version without redeploying the agent), overwrite the env var manually:

```bash
azd env set TOOLBOX_MY_TOOLS_MCP_ENDPOINT \
  "https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/my-tools/mcp?api-version=v1"
```

Trade-off: agents now pick up new default versions automatically, but azd's deploy-time version pinning is bypassed. The default version of a new toolbox is `v1` until you promote another version via the SDK / REST.

## Required header

Every request to the toolbox MCP endpoint must include:

```http
Foundry-Features: Toolboxes=V1Preview
```

The agents extension sends this header automatically when calling the management API. Your agent code MUST send it on every MCP call -- see `consume`.

## When NOT to use a toolbox

* The agent only needs a single built-in tool. `web_search`, `code_interpreter`, `function` work without a toolbox, depending on the runtime.
* You need per-user isolation for `code_interpreter` or `file_search`. Toolbox-hosted versions share container / vector store across users in the project; use the direct (non-toolbox) tool form when isolation matters.
* The tools are owned by another team and you only want to consume an existing toolbox. Skip the `toolboxes[]` block; set the consumer-endpoint URL manually as an env var.

## Where to go next

* "How do I add a toolbox with X / Y / Z tools?" -> `add`
* "What fields does the `mcp` / `openapi` / `azure_ai_search` tool take?" -> `tools`
* "How does my agent code call the toolbox at runtime?" -> `consume`
* "How do I add a connection that a toolbox tool references?" -> `azd ai doc connection add`
