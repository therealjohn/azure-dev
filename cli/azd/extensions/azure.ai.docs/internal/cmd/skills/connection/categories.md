---
short: Reference of connection categories (slug -> ARM-canonical mapping).
order: 30
---
# Connection categories

Reference for the `category:` field on a connection in `azure.yaml` `services.<name>.config.connections[]` AND for the `--kind` flag on `azd ai agent connection create`.

The CLI accepts both kebab-case slugs (for typing convenience) and ARM-canonical PascalCase. Both forms accept the same connection. `azure.yaml` is normalized to ARM-canonical at write time.

---

## Categories accepted by `azd ai agent`

This is the subset of categories the parser recognizes today and the most common scenarios for each. The full ARM enum is larger, but anything outside this list is unlikely to round-trip cleanly through the agent extension.

| ARM-canonical (`category:`)  | CLI slug (`--kind`)            | What it points at                                                        |
| ---------------------------- | ------------------------------ | ------------------------------------------------------------------------ |
| `RemoteTool`                 | `remote-tool`                  | MCP server, OpenAPI-backed tool, A2A peer agent. Most custom-tool work.  |
| `RemoteA2A`                  | `remote-a2a`                   | A2A peer agent (explicit A2A category; `RemoteTool` also works).         |
| `CognitiveSearch`            | `cognitive-search`             | Azure AI Search service (RAG via the `azure_ai_search` tool).            |
| `GroundingWithBingSearch`    | `grounding-with-bing-search`   | Bing search account (for the `bing_grounding` tool).                     |
| `BingLLMSearch`              | (no slug)                      | Newer Bing LLM-search category (for the `web_search` tool when scoped).  |
| `AIServices`                 | `ai-services`                  | Azure AI Services multi-service account.                                 |
| `AzureOpenAI`                | (no slug)                      | Azure OpenAI deployment (used by model resources, not tool resources).   |
| `CognitiveService`           | (no slug)                      | Single-purpose Cognitive Service account.                                |
| `ApiKey`                     | `api-key`                      | Generic API-key-protected HTTP endpoint.                                 |
| `CustomKeys`                 | `custom-keys`                  | Endpoint with multiple custom header / parameter values (Authorization + region + tenant, etc.). |
| `OAuth2`                     | (no slug; use authType)        | OAuth2-protected endpoint.                                               |
| `AppInsights`                | `app-insights`                 | Application Insights resource (for telemetry connections).               |
| `ContainerRegistry`          | `container-registry`           | Azure Container Registry (image source).                                 |
| `MicrosoftOneLake`           | (no slug)                      | OneLake workspace.                                                       |
| `AzureBlob` / `AzureSqlDb` / `AzureSynapseAnalytics` / `AzureMySqlDb` / `AzurePostgresDb` / `ADLSGen2` / `AzureDataExplorer` | (no slug) | Azure data services.                                       |
| `Git`                        | (no slug)                      | Git repository (e.g. for dataset versioning).                            |
| `Redis`                      | (no slug)                      | Redis cache.                                                             |
| `S3`                         | (no slug)                      | AWS S3 bucket.                                                           |
| `Snowflake`                  | (no slug)                      | Snowflake warehouse.                                                     |
| `Serverless`                 | (no slug)                      | Serverless endpoint (Foundry serverless model).                          |
| `Elasticsearch`              | (no slug)                      | Elasticsearch cluster.                                                   |
| `Pinecone`                   | (no slug)                      | Pinecone vector DB.                                                      |
| `Qdrant`                     | (no slug)                      | Qdrant vector DB.                                                        |

For categories with no CLI slug, pass the ARM-canonical form to `--kind` directly (e.g. `--kind BingLLMSearch`). The slug list is implemented in `normalizeKind` in the agents extension; the source of truth for additions is that function.

---

## Picking a category

Decision tree by scenario:

* **MCP server** -- `RemoteTool`. Always.
* **Azure AI Search** -- `CognitiveSearch`. Pair with the `azure_ai_search` tool (in `resources[]` if outside a toolbox, in `toolboxes[].tools[]` if inside).
* **Bing grounding** -- `GroundingWithBingSearch`. Pair with the `bing_grounding` tool. If you just need "search the web" without specific Bing-grounding semantics, use the built-in `web_search` tool in a toolbox -- that needs no connection at all.
* **Plain HTTP API with a static key in a header** -- `ApiKey`. The credential goes in `credentials.key` and the runtime sends it as `Authorization: Bearer <key>` by default. If you need a different header / parameter name (or multiple), use `CustomKeys` instead.
* **HTTP API with multiple keyed headers (`Authorization`, region, etc.)** -- `CustomKeys`. Each entry in `credentials.keys` becomes a header.
* **OpenAPI-spec'd backend** -- whatever its auth requires (`ApiKey`, `CustomKeys`, `OAuth2`). Pair with the `openapi` tool inside a toolbox.
* **Another deployed agent (A2A)** -- `RemoteTool` is the common choice; `RemoteA2A` is the explicit alternative.

---

## Cross-references

* `authType` -- separate axis from `category`. Some combinations are valid (e.g. `RemoteTool` + `OAuth2`); some are not (e.g. `CognitiveSearch` + `OAuth2` -- search doesn't support OAuth2). See `auth-types`.
* `target` -- the URL or ARM resource ID the category points at. For `ARM`-flavored categories (e.g. `ContainerRegistry`, `AzureBlob`), this can be the resource ID; for HTTP-flavored ones (`RemoteTool`, `ApiKey`), it's a URL.
* `metadata` -- per-category extra data. `CognitiveSearch` typically takes `indexName`; `Git` takes a branch / ref; `Redis` takes a port.

---

## Don't see the category you need?

Two options:

1. **Use the imperative `azd ai agent connection create --kind <ARM-canonical>`** -- the CLI passes the kind through to ARM, so anything ARM accepts works even if `azure.yaml` doesn't have first-class support for it.
2. **File an issue** -- if a category needs better declarative support (specific `metadata` keys recognized, sensible default credentials shape), it's a docs + parser fix in the agents extension.
