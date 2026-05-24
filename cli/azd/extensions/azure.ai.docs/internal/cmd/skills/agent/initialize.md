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

### Resolving subscription / location when `project show` is empty

If you still need a subscription or location (e.g. the human has not
chosen a Foundry project yet and you need to seed `--project-id` or a
`provision` location), keep using `azd` -- do NOT shell out to `az`:

1. `azd config get defaults` -- returns the user-level azd defaults
   as JSON: `{ "location": "...", "subscription": "..." }`. These are
   the same defaults the interactive prompts seed.
2. `azd env get-values` -- the active azd environment's variables
   (look for `AZURE_SUBSCRIPTION_ID`, `AZURE_LOCATION`,
   `AZURE_AI_PROJECT_ENDPOINT`).
3. Ask the human.
4. Last resort: `az account list --output json` -- only after 1-3 are
   exhausted AND the human has approved the shell-out. Users who
   picked `azd` typically did so to avoid juggling two CLIs.

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

First, decide which path you are on. This decision drives every remaining
flag.

| User is ... | Signal                                                              | Source flag                  |
| ----------- | ------------------------------------------------------------------- | ---------------------------- |
| Greenfield  | Empty workspace, only a bootstrap stub, or wants a starter          | `-m <manifestUrl>` (default) |
| Brownfield  | The cwd already contains hand-written agent source the user owns    | `--from-code`                |

The interactive picker (no `-m`, no `--from-code`) is for human-driven
flows only. NEVER use it under `--no-prompt`.

### Greenfield: start from a curated sample (the common case)

Run `azd ai agent sample list` first (see the `samples` topic) to fetch a
`manifestUrl` from the curated catalog. Do NOT guess or hand-author a
manifest URL.

```bash
# 1. Discover a manifest URL
azd ai agent sample list --featured-only --language python --output json

# 2. Init with the picked manifestUrl
azd ai agent init --no-prompt \
  --project-id "<projectResourceId>" \
  -m "<manifestUrl-from-sample-list>"
```

`-m` accepts a URL or a local path; the value comes from the `manifestUrl`
field of `azd ai agent sample list --output json`.

### Brownfield: existing agent source (rare)

ONLY use `--from-code` when the workspace already contains hand-written
agent source the user wants lifted into a hosted Foundry agent.

```bash
azd ai agent init --no-prompt \
  --project-id "<projectResourceId>" \
  --from-code \
  --deploy-mode code \
  --runtime python_3_13 \
  --entry-point app.py
```

`--runtime` and `--entry-point` are required with
`--deploy-mode code --no-prompt`. `--deploy-mode container` (the default)
builds from `Dockerfile`.

Full flag set:

- `-m, --manifest <url-or-path>` -- agent manifest source (greenfield
  default). Mutually exclusive with `--from-code`. Get candidates from
  `azd ai agent sample list --output json` (the `manifestUrl` field).
- `--from-code` -- use the code in cwd as the agent source.
  BROWNFIELD ONLY -- requires hand-written agent source already in the
  workspace. Mutually exclusive with `-m`. Do NOT pass this just because
  `--no-prompt` complains about a missing source; pick a sample with
  `-m` instead.
- `-p, --project-id <resourceId>` -- Foundry project ARM ID. Required
  in `--no-prompt`. If the human has not given you one, STOP and ask
  them: "Open the Foundry portal at https://ai.azure.com -> Operate ->
  Admin -> select your project -> Copy the Resource ID." Do NOT shell
  out to `az cognitiveservices ...` to discover it.
- `--agent-name <name>` -- Foundry agent name written to `agent.yaml`.
  Reusing a name creates a new version of the existing agent.
- `--model <name>` -- model id (e.g. `gpt-4.1-mini`). Defaults to
  `gpt-4.1-mini`. Mutually exclusive with `--model-deployment`
  (`--model-deployment` wins if both are given).
- `-d, --model-deployment <name>` -- name of an existing model
  deployment on the Foundry project. Only valid when paired with
  `--project-id`.
- `--deploy-mode container|code` -- defaults to `container` in
  `--no-prompt`. `container` builds from `Dockerfile`; `code` ZIPs the
  source and Foundry builds the runtime.
- `--runtime <id>` -- e.g. `python_3_13`, `python_3_14`, `dotnet_10`,
  `node_22`. REQUIRED with `--deploy-mode code --no-prompt`.
- `--entry-point <file>` -- e.g. `app.py`, `MyAgent.dll`,
  `dist/index.js`. REQUIRED with `--deploy-mode code --no-prompt`.
- `--dep-resolution remote_build|bundled` -- defaults to
  `remote_build`. Only relevant for code deploy.
- `--protocol <name>` (repeatable) -- e.g. `responses`, `invocations`.
- `-s, --src <dir>` -- where to download the agent definition (defaults
  to `src/<agent-id>`).
- `--force` -- required together with `--no-prompt` when init would
  otherwise need confirmation (e.g. an input manifest already lives
  inside the generated `src` tree).
- `--no-prompt` -- refuses interactive prompts; flags must supply every
  required value, otherwise the command emits a structured
  `validation` error that names the missing flag.
- `-o, --output json` -- machine-readable progress (when supported).

Init writes files into the working directory. There is no confirmation
envelope on init -- it's a non-destructive create. Files written:

- `azure.yaml` (or appends a new ai.agent service to an existing one)
- `<service-dir>/agent.yaml`
- `<service-dir>/.agentignore` (code-deploy only; controls ZIP packaging)

After init, re-run Step 1 + Step 2 to confirm the new state. For the
ON-DISK shape of `agent.yaml`, see the `extend` topic.

----------------------------------------------------------------------

## Step 3b -- The workspace already has azure.yaml but no agent service

The `--help` preamble of `azd ai agent` will tell you this case. Use the
same init invocation as Step 3a. The new service is appended to
`azure.yaml`.

----------------------------------------------------------------------

## Step 3c -- Service exists, no Foundry project endpoint

You need Azure resources provisioned. This is NOT an `azd ai agent`
command -- use core azd:

```bash
azd provision --no-prompt
```

After provision succeeds, re-run Step 1; `projectEndpoint` should
populate. Full deploy lifecycle (provision + deploy + verify) lives in
the `deploy` topic.

----------------------------------------------------------------------

## Step 3d -- Provisioned but not deployed

```bash
azd deploy --no-prompt
```

After deploy succeeds, `azd ai agent show --output json` will return the
agent record (Step 2's "active" shape). At that point the `develop`,
`configure`, `extend`, `evaluate`, `operate`, and `investigate` topics
all become applicable.

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
