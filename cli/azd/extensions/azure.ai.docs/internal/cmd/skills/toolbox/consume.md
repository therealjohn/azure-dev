---
short: How agent code consumes the toolbox MCP endpoint at runtime.
order: 40
---
# Toolbox consume: agent-side runtime wiring

How the running agent reaches its toolbox: the env vars that hold the URL, the header every call must include, the MCP client gotchas, and the per-runtime patterns.

For the toolbox-side definition (defining one in `azure.yaml`), see `add`. For lifecycle and versioning, see `overview`.

## Env vars

When you declare a toolbox under `services.<name>.config.toolboxes[]`, `azd deploy`:

1. Creates a new toolbox version on Foundry.
2. Writes the version-pinned MCP URL to the azd environment as `TOOLBOX_<NAME>_MCP_ENDPOINT`.
3. Init (one-time) added a matching reference to `<service>/agent.yaml` under `environment_variables[]`, so the deployed agent reads it from the platform environment.

When hosted on Foundry (after `azd up`), the platform also injects:

* `FOUNDRY_AGENT_TOOLBOX_ENDPOINT` -- base URL (no toolbox name). Construct per-toolbox URLs as `${FOUNDRY_AGENT_TOOLBOX_ENDPOINT}/{name}/mcp?api-version=v1` if you need to.

Local runs (`azd ai agent run`) get only `TOOLBOX_<NAME>_MCP_ENDPOINT`.

Naming rule -- the toolbox name is uppercased and non-alphanumeric characters collapse to `_`:

| Toolbox name      | Env var                                |
| ----------------- | -------------------------------------- |
| `my-tools`        | `TOOLBOX_MY_TOOLS_MCP_ENDPOINT`        |
| `agent.tools.v2`  | `TOOLBOX_AGENT_TOOLS_V2_MCP_ENDPOINT`  |
| `Web-Search:V2`   | `TOOLBOX_WEB_SEARCH_V2_MCP_ENDPOINT`   |

URL format (version-pinned, what azd writes by default):

```
https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/<name>/versions/<version>/mcp?api-version=v1
```

Consumer endpoint (you set manually if you want auto-promotion -- see `overview`):

```
https://<account>.services.ai.azure.com/api/projects/<project>/toolboxes/<name>/mcp?api-version=v1
```

## Required header

```http
Foundry-Features: Toolboxes=V1Preview
```

Without this header, every MCP request fails. Set it once on your client.

## Token scope

When acquiring a token for the toolbox endpoint, use:

```
https://ai.azure.com/.default
```

## MCP client gotchas

Foundry's toolbox MCP endpoint has a couple of quirks. If you're using a generic MCP client, set these or the connection won't work:

* **Always stream.** Non-streaming mode (`stream=False` or equivalent) is NOT supported. Use the streamable HTTP transport.
* **Don't call `prompts/list`.** The Foundry MCP server doesn't implement it; the call returns `500`. Many MCP clients call it automatically at startup -- pass `load_prompts=False` (or the equivalent option in your client) to disable.
* **Don't call `send_ping()`.** Same reason. Microsoft Agent Framework's `MCPStreamableHTTPTool._ensure_connected()` does this automatically; override it or disable the ping check.
* **MCP tool names are prefixed with `server_label`.** A tool `get_info` on `server_label: myserver` is exposed as `myserver.get_info`. The Copilot SDK rejects dots in tool names and requires you to map `myserver.get_info` <-> `myserver_get_info` at the bridge layer.

## Two consumption patterns

### Server-side (Foundry runs the tools)

Your agent uses the Foundry SDK's Responses or Invocations API, includes the toolbox endpoint in its tool list, and Foundry executes tool calls server-side. The agent code never opens an MCP client itself.

Use when: writing a hosted agent against the Foundry Responses API, or you want the platform to handle auth, retries, and observability.

### Client-side (the agent code calls MCP directly)

Your agent reads `TOOLBOX_<NAME>_MCP_ENDPOINT`, opens an MCP session, lists the tools, and includes them in its own tool-calling loop (LangGraph, LangChain, Agent Framework, GitHub Copilot SDK, custom code).

Use when: bringing your own runtime, or you want fine-grained control over tool invocation, approval policies, or post-processing.

## Minimal client-side example (Python)

```python
import os
import asyncio
from azure.identity import DefaultAzureCredential
from mcp.client.streamable_http import streamablehttp_client
from mcp import ClientSession

async def main():
    url = os.environ["TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT"]
    token = DefaultAzureCredential().get_token("https://ai.azure.com/.default").token
    headers = {
        "Authorization": f"Bearer {token}",
        "Foundry-Features": "Toolboxes=V1Preview",
    }
    async with streamablehttp_client(url, headers=headers) as (read, write, _):
        async with ClientSession(read, write) as session:
            await session.initialize()
            tools = (await session.list_tools()).tools
            print(f"Tools found: {len(tools)}")
            for t in tools:
                print(f"  - {t.name}: {(t.description or '')[:80]}")
            # result = await session.call_tool("<tool_name>", arguments={...})

asyncio.run(main())
```

Install: `pip install mcp azure-identity`.

## Per-runtime wiring (.env shapes)

### LangGraph

`.env`:

```
FOUNDRY_AGENT_TOOLBOX_ENDPOINT=https://<account>.services.ai.azure.com/api/projects/<project>
FOUNDRY_AGENT_TOOLBOX_FEATURES=Toolboxes=V1Preview
TOOLBOX_NAME=agent-tools
AZURE_AI_MODEL_DEPLOYMENT_NAME=gpt-4o
```

Use `langchain_azure_ai.tools.AzureAIProjectToolbox` (requires `langchain-azure-ai[tools]>1.2.3`):

```python
from langchain_azure_ai.tools import AzureAIProjectToolbox

toolbox = AzureAIProjectToolbox(toolbox_name=TOOLBOX_NAME)
tools = await toolbox.get_tools()
```

### Microsoft Agent Framework

Use `MCPStreamableHTTPTool` against the toolbox endpoint. Auth via an `httpx.AsyncClient` with a bearer-token provider; always set the `Foundry-Features` header at the client level.

### GitHub Copilot SDK

Open an MCP session against the endpoint, list the tools, and bridge them into Copilot SDK tool definitions. Replace `.` with `_` in tool names (Copilot rejects dots).

## Required RBAC

The calling identity needs the **Foundry User** role on the Foundry project. Three identities matter:

* **Developer** -- the human / pipeline that creates and updates toolbox versions (i.e. whoever runs `azd deploy`).
* **Agent identity** -- the hosted agent's managed identity that calls tools at runtime.
* **End user** -- only when `UserEntraToken` or OAuth connections are involved (the user's identity is proxied through).

## Tool call argument shapes

A small per-tool-type reference. Argument names are easy to get wrong:

| Tool type        | `tools/call` arguments                                                 |
| ---------------- | ---------------------------------------------------------------------- |
| Web Search       | `{"search_query": "weather in seattle"}`                               |
| Azure AI Search  | `{"query": "search text"}`                                             |
| File Search      | `{"queries": ["search text"]}`  (plural; takes an array)               |
| Code Interpreter | `{"code": "print(2 ** 100)"}`                                          |
| A2A              | `{"message": {"parts": [{"type": "text", "text": "Hello"}]}}`          |
| MCP              | Whatever the underlying MCP tool's `inputSchema.properties` defines.   |

Inspect each tool's `inputSchema` (returned by `tools/list`) to confirm the exact parameter names.

## Verifying the connection

```bash
TOKEN=$(az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv)
curl -sS "$TOOLBOX_AGENT_TOOLS_MCP_ENDPOINT" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Foundry-Features: Toolboxes=V1Preview" \
  -H "Accept: application/json, text/event-stream" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

A `200` with a JSON-RPC body listing the tools means the wire is intact. Each tool has `name`, `description`, `inputSchema`, and a `_meta.tool_configuration` block (for MCP tools, this includes `require_approval`).

## Handling `require_approval`

For MCP tools, each `tools/list` entry includes `_meta.tool_configuration.require_approval`. Values:

* `"always"` -- the agent runtime must prompt the user for confirmation before EVERY invocation.
* `"never"` -- the agent can invoke freely.

The toolbox MCP proxy does NOT enforce this -- it always executes `tools/call`. Gating is YOUR agent runtime's responsibility. Build an approval map at startup from `tools/list` and check it before each call.

## Troubleshooting

| Symptom                                                | Likely cause                                                                                |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| `TOOLBOX_<NAME>_MCP_ENDPOINT` not set locally          | No `azd deploy` since the toolbox was added. Run `azd deploy`.                              |
| `400` with `Toolboxes` in the message                  | Missing `Foundry-Features: Toolboxes=V1Preview` header.                                     |
| `400 Multiple tools without identifiers`               | Two unnamed tool types in one toolbox. Give each a unique `name:`.                          |
| `401` on MCP calls                                     | Expired token or wrong scope. Use `https://ai.azure.com/.default`.                          |
| `403 Forbidden`                                        | Caller missing `Foundry User` role; or for `UserEntraToken`, the user lacks rights on the downstream service. |
| `404` on the version-pinned URL                        | Version was deleted out-of-band. Re-deploy or switch to the consumer URL.                   |
| `500` on `prompts/list`                                | The Foundry MCP server doesn't implement it. Pass `load_prompts=False` to your MCP client.  |
| `500` on `send_ping()`                                 | Same -- disable the ping in your client (Agent Framework: override `_ensure_connected`).    |
| `500` with non-streaming `tools/call`                  | Non-streaming not supported. Use `stream=True` / streamable HTTP transport.                 |
| `500` on `tools/list`                                  | Transient. Retry after a few seconds.                                                       |
| `CONSENT_REQUIRED` (`-32007`)                          | OAuth connection needs user consent. Open the URL from `error.message`; retry afterwards.   |
| `tools/list` returns zero tools (MCP / A2A / OpenAPI)  | Invalid connection credentials or malformed OpenAPI spec. Verify the `project_connection_id` and re-check the spec / auth. |
| `tools/list` returns zero tools (other built-ins)      | Toolbox not fully provisioned, or tool type unsupported in your region. Wait 10s and retry. |
| Tool names don't match what the model called           | MCP tool names are prefixed with `server_label.`. Use `{server_label}.{tool_name}`.        |
| Custom env vars overwritten                            | The platform reserves the `FOUNDRY_` prefix. Don't use it for your own values.              |
