---
short: Bootstrap a new Foundry agent project end-to-end.
order: 10
---
# Initialize: bootstrap a Microsoft Foundry agent project with azd

Audience: an AI coding assistant driving the `azd ai agent` extension on
behalf of a developer. Every command below is safe to run from a script.

The path through this topic is linear:

1. Verify identity and context.
2. Verify what (if anything) is already deployed.
3. Branch into `azd ai agent init`, `azd init`, `azd provision`, or `azd deploy`
   based on what step 2 reports.

----------------------------------------------------------------------

## Step 1 -- Verify identity and project context

ALWAYS run this BEFORE any other agent command, even read-only ones. It
tells you which Azure subscription, tenant, and Foundry project endpoint
the rest of the workflow will target.

```bash
azd ai agent project show --output json
```

Success payload (all fields optional; trust the keys you see):

```json
{
  "subscription": { "id": "11111111-2222-3333-4444-555555555555" },
  "tenant": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "location": "eastus2",
  "resourceGroup": "rg-myteam",
  "projectEndpoint": "https://contoso.services.ai.azure.com/api/projects/myproj",
  "projectEndpointSource": "azdEnv",
  "foundryAccount": "contoso",
  "projectName": "myproj"
}
```

`projectEndpointSource` is one of `flag`, `azdEnv`, `globalConfig`, or
`foundryEnv`. Empty payload (e.g. `{}`) means none of the cascade levels
produced a value -- branch to `azd ai agent init` (Step 3a) without
asking the human.

Exit codes: `0` always (this command never fails when ANY field can be
resolved). A nonzero exit means the azd host itself is unreachable; show
the error to the human.

----------------------------------------------------------------------

## Step 2 -- Verify what's already deployed

```bash
azd ai agent show --output json
```

Two possible shapes. Branch on `.status`.

`status: "not_deployed"` -- no agent yet. The payload includes a
`next_step` block telling you exactly what to run next:

```json
{
  "agent": null,
  "status": "not_deployed",
  "service": "echo",
  "next_step": {
    "suggestions": [
      {
        "command": "azd ai agent project show --output json",
        "description": "Inspect identity, subscription, and project context."
      },
      {
        "command": "azd deploy",
        "description": "Deploy agent service \"echo\"."
      }
    ]
  }
}
```

`status: "active"` (or any other API status) -- the agent is deployed.
You will receive the full agent record:

```json
{
  "id": "agent-id",
  "name": "echo",
  "version": "3",
  "status": "active",
  "agent_endpoints": {
    "Responses": "https://contoso.services.ai.azure.com/api/projects/myproj/agents/echo/endpoint/protocols/openai/responses?api-version=..."
  },
  "playground_url": "https://ai.azure.com/..."
}
```

Either way, this command exits 0. Branch on the payload, never on the
exit code.

----------------------------------------------------------------------

## Step 3a -- Initialize a new agent project

Run from the directory where you want the project files written.

```bash
azd ai agent init --no-prompt --template <template-name>
```

Common flags:

- `--template <name>` -- skips the interactive picker. Use
  `azd ai agent sample list --output json` to enumerate templates.
- `--no-prompt` -- refuses interactive prompts; flags must supply every
  required value, otherwise the command emits a structured
  `validation` error that names the missing flag.
- `--output json` -- machine-readable progress (when supported).

Init writes files into the working directory. There is no confirmation
envelope on init -- it's a non-destructive create. Files written:

- `azure.yaml` (or appends a new ai.agent service to an existing one)
- `<service-dir>/agent.yaml`
- `<service-dir>/skills/...` (optional)

After init, re-run Step 1 + Step 2 to confirm the new state.

----------------------------------------------------------------------

## Step 3b -- The workspace already has azure.yaml but no agent service

The `--help` preamble of `azd ai agent` will tell you this case. Run:

```bash
azd ai agent init --no-prompt --template <template-name>
```

The new service is appended to azure.yaml.

----------------------------------------------------------------------

## Step 3c -- Service exists, no Foundry project endpoint

You need Azure resources provisioned. This is NOT an `azd ai agent`
command -- use core azd:

```bash
azd provision --no-prompt
```

After provision succeeds, re-run Step 1; `projectEndpoint` should
populate.

----------------------------------------------------------------------

## Step 3d -- Provisioned but not deployed

```bash
azd deploy --no-prompt
```

After deploy succeeds, `azd ai agent show --output json` will return the
agent record (Step 2's "active" shape) and the `configure` /
`investigate` / `operate` skills become applicable.

----------------------------------------------------------------------

## Common error codes

When any command exits 1, the stderr JSON has a `code` field. The codes
you're most likely to see during initialize:

- `not_logged_in` / `login_expired` -- run `azd auth login`, then retry.
- `missing_project_endpoint` -- the 5-level cascade produced nothing.
  Either run `azd provision` or `azd env set AZURE_AI_PROJECT_ENDPOINT
  <url>` if you have an endpoint to inject.
- `project_not_found` -- the working directory has no azure.yaml. Move
  to the project root or run Step 3a.
- `azd_client_failed` -- the azd host itself is not running. Surface to
  the human.

Any unfamiliar `code` value is safe to surface verbatim to the human.

----------------------------------------------------------------------

## Diagnostics

When something doesn't add up, run the full health check:

```bash
azd ai agent doctor --output json
```

`status: "fail"` checks include a `suggestion` field. Each check is
independent -- fix one, re-run doctor, iterate. Exit code is `0` if at
least one check passed and none failed; `1` if any failed; `2` if all
were skipped (e.g. no project detected).
