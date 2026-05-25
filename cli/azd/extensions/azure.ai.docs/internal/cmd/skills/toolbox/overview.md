---
short: What a toolbox is, how azd handles it, and the deploy lifecycle.
order: 10
---
# Toolbox overview

A **toolbox** is a curated bundle of tools that Foundry exposes as a single MCP-compatible endpoint. The agent connects to one URL and dynamically discovers every tool inside. Toolboxes are the recommended way to attach multiple tools to an agent -- one endpoint, central credential handling, no per-agent tool wiring.

Per the Foundry tool catalog: built-in tools (`web_search`, `code_interpreter`, `file_search`), MCP servers, OpenAPI APIs, Azure AI Search, Bing grounding, and A2A peer agents can all live in the same toolbox.

For step-by-step recipes, see `add`. For the full tool-type reference, see `tools`. For how agent code uses the endpoint, see `consume`.

## How azd handles toolboxes

Toolboxes are **declarative**. You define one under `services.<name>.config.toolboxes[]` in `azure.yaml`; azd takes over from there.

| Stage              | What happens                                                                                          |
| ------------------ | ----------------------------------------------------------------------------------------------------- |
| `azd ai agent init`| When the seed manifest has `kind: toolbox`, init writes it into `azure.yaml` and adds a matching `TOOLBOX_<NAME>_MCP_ENDPOINT` env var to the on-disk `agent.yaml`. |
| `azd provision`    | Bicep creates / updates any connections under `connections[]` / `toolConnections[]` referenced by the toolbox's tools. |
| `azd deploy`       | The agents extension calls `POST /toolboxes/{name}/versions?api-version=v1` for every toolbox in the service config. **A new version is created on every deploy** -- the toolbox itself is auto-created if it didn't exist. |
| Post-deploy        | The version-pinned MCP URL is written back to the azd environment as `TOOLBOX_<NAME>_MCP_ENDPOINT`. The deployed agent's `environment_variables` already references `${TOOLBOX_<NAME>_MCP_ENDPOINT}`, so the running agent gets the new URL. |

Two consequences worth knowing:

* **You don't manage toolbox creation or versions through azd's CLI** -- there's no `azd ai agent toolbox` command. The lifecycle is implicit in deploy.
* **Every `azd deploy` produces a new version** of every toolbox in the project. Old versions stay on Foundry until you delete them via the SDK / REST / Foundry Toolkit. Agents deployed by azd always run against the latest version (it's the one in the env var).

## Toolbox vs. consumer endpoints

Foundry exposes two endpoint patterns for any toolbox:

| Endpoint                                                                  | When                                                  |
| ------------------------------------------------------------------------- | ----------------------------------------------------- |
| `{project}/toolboxes/{name}/versions/{version}/mcp?api-version=v1`        | Version-pinned (developer / version-specific).        |
| `{project}/toolboxes/{name}/mcp?api-version=v1`                           | Default version (consumer).                           |

azd-deployed agents use the **version-pinned** form so deploys are reproducible -- the env var `TOOLBOX_<NAME>_MCP_ENDPOINT` always holds the version-specific URL of the version azd just published.

If you want consumer-endpoint behavior (auto-pickup of a new default version without redeploying the agent), set the env var manually to the consumer URL after deploy:

```bash
azd env set TOOLBOX_MY_TOOLS_MCP_ENDPOINT \
  "https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/my-tools/mcp?api-version=v1"
```

The trade-off: agents now pick up new default versions automatically, but azd's deploy-time version pinning is bypassed.

## Required header

Every request to the toolbox MCP endpoint must include:

```http
Foundry-Features: Toolboxes=V1Preview
```

The agents extension sends this header automatically when calling the management API. Your agent code MUST send it on every MCP call to the runtime endpoint -- see `consume`.

## When NOT to use a toolbox

* You only need a single built-in tool with no connection (e.g. just `web_search`). It still works inside a toolbox, but you can also add it as a direct `resources[]` entry (`bing_grounding`, `azure_ai_search`) or skip the resource entirely for `web_search` / `code_interpreter` / `file_search` -- the Foundry runtime exposes them when the agent code requests them.
* The tools are managed by another team and you only want to consume an existing toolbox. Skip the `toolboxes[]` block; reference the existing toolbox's consumer endpoint by env var only.

## Where to go next

* "How do I add a toolbox with X / Y / Z tools?" -> `add`
* "What fields does the `mcp` / `openapi` / `azure_ai_search` tool take?" -> `tools`
* "How does my agent code call the toolbox at runtime?" -> `consume`
* "How do I add a connection that a toolbox tool references?" -> `azd ai doc connection add`
