---
short: How agent code consumes the toolbox MCP endpoint at runtime.
order: 40
---
# Toolbox consume: agent-side runtime wiring

How the running agent reaches its toolbox: the env var that holds the URL, the header every call must include, and the basic MCP client pattern.

For deployment-side wiring (defining the toolbox in `azure.yaml`), see `add`. For lifecycle and versioning, see `overview`.

## The env var

When you declare a toolbox under `services.<name>.config.toolboxes[]`, `azd deploy`:

1. Creates a new toolbox version on Foundry.
2. Writes the version-pinned MCP URL to the azd environment as `TOOLBOX_<NAME>_MCP_ENDPOINT`.
3. (At init time only) added a matching reference to `<service>/agent.yaml` under `environment_variables[]`, so the deployed agent reads it from the platform environment.

Naming: the toolbox name is uppercased and non-alphanumeric characters collapse to `_`. Examples:

| Toolbox name      | Env var                                |
| ----------------- | -------------------------------------- |
| `my-tools`        | `TOOLBOX_MY_TOOLS_MCP_ENDPOINT`        |
| `agent.tools.v2`  | `TOOLBOX_AGENT_TOOLS_V2_MCP_ENDPOINT`  |
| `Web-Search:V2`   | `TOOLBOX_WEB_SEARCH_V2_MCP_ENDPOINT`   |

URL format (version-pinned, what azd writes by default):

```
https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/<name>/versions/<version>/mcp?api-version=v1
```

Consumer endpoint (you set manually if you want auto-promotion of new default versions; see `overview`):

```
https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/<name>/mcp?api-version=v1
```

## Required header on every call

```http
Foundry-Features: Toolboxes=V1Preview
```

Without this header, every MCP request fails. Set it once on your client; the platform calls all include it on your behalf only when going through Foundry SDKs.

## Two consumption patterns

### Server-side (Foundry runs the tools)

The simpler model: your agent uses the Foundry SDK's responses or invocations API, includes the toolbox endpoint in its tools list, and Foundry executes tool calls server-side and returns results in the response stream. The agent code never spins up an MCP client itself.

Use when:

* You're writing a hosted agent against the Foundry Responses API.
* You want the platform to handle auth, retries, and observability for tool calls.

### Client-side (the agent code calls MCP directly)

The flexible model: your agent reads `TOOLBOX_<NAME>_MCP_ENDPOINT`, opens an MCP session, lists the tools, and includes them in its own tool-calling loop (LangGraph, LangChain, Agent Framework, custom code).

Use when:

* You're bringing your own runtime (LangGraph, GitHub Copilot SDK, Claude Agent SDK, etc.).
* You want fine-grained control over tool invocation, approval policies, or post-processing.

## Minimal client-side example (Python)

```python
import os
import asyncio
from azure.identity import DefaultAzureCredential
from mcp.client.streamable_http import streamablehttp_client
from mcp import ClientSession

async def main():
    endpoint = os.environ["TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT"]
    cred = DefaultAzureCredential()
    token = cred.get_token("https://ai.azure.com/.default")
    headers = {
        "Authorization": f"Bearer {token.token}",
        "Foundry-Features": "Toolboxes=V1Preview",
    }
    async with streamablehttp_client(endpoint, headers=headers) as (read, write, _):
        async with ClientSession(read, write) as session:
            await session.initialize()
            tools = (await session.list_tools()).tools
            # ... pass tools to your model / loop
            # await session.call_tool(name, arguments={...})

asyncio.run(main())
```

Install: `pip install mcp azure-identity`.

## Locally vs hosted

Two run modes have the same env var contract but different injection paths:

| Run mode          | How `TOOLBOX_<NAME>_MCP_ENDPOINT` gets set                                                  |
| ----------------- | -------------------------------------------------------------------------------------------- |
| `azd ai agent run`| azd loads the active environment's value (set at the most recent `azd deploy`) and exports it. |
| Hosted (`azd up`) | The platform injects the value from the agent's `environment_variables[]` declaration in `agent.yaml`. |

Either way, the agent code reads `os.environ[...]` (or your language's equivalent) -- no special SDK call needed.

## Verifying the connection

Quick smoke test from a shell with the env var set:

```bash
TOKEN=$(az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv)
curl -sS "$TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Foundry-Features: Toolboxes=V1Preview" \
  -H "Accept: application/json, text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

A `200` response with a JSON-RPC body listing the tools means the wire is intact. A `403` usually means the calling identity lacks the `Foundry User` role on the project; a `400` with `Toolboxes` in the message usually means the feature header is missing.

## Troubleshooting

| Symptom                                          | Likely cause                                                                                  |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------- |
| Env var not set when running locally             | No `azd deploy` since the toolbox was added. Run `azd deploy`.                                |
| `400` "Toolboxes feature" in the message         | Missing `Foundry-Features: Toolboxes=V1Preview` header.                                        |
| `403 Forbidden`                                  | Calling identity lacks the `Foundry User` role on the project, or (for `UserEntraToken` connections) the user lacks rights on the downstream service. |
| Tool calls succeed but return empty results      | Tool needs a connection that wasn't provisioned. Run `azd ai agent connection list --output json` and verify the `project_connection_id` is present. |
| `404` when calling the version-pinned URL        | The version was deleted out-of-band. Re-deploy or switch to the consumer URL (see `overview`). |
