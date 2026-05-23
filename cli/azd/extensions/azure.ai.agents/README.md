# Azure Developer CLI (azd) Agents Extension

## Agent-friendly CLI

Every data-returning leaf command accepts `--output json` (alias `-o json`) for
machine-readable output, alongside the human-readable default. Examples:

```bash
azd ai agent show --output json
azd ai agent doctor --output json
azd ai agent eval list --output json
azd ai agent eval show <eval-id> --output json
azd ai agent optimize list --output json
azd ai agent optimize status <op-id> --output json
azd ai agent connection list --output json
azd ai agent connection show <name> --output json
azd ai agent session list --output json
azd ai agent files list --output json
azd ai agent sample list --output json
```

Set `AZD_NO_PROMPT=1` (or pass `--no-prompt` to the parent `azd` invocation)
when running from an agent or non-interactive script. Commands that would
otherwise prompt for missing values return a structured `validation` error
naming the flag(s) the caller should pass instead.

### Exit codes

| Code | Meaning |
| ---- | ------- |
| `0`  | Success. |
| `1`  | Error. The command emitted a structured error describing the cause. |
| `2`  | Reserved for an upcoming confirmation-required protocol on write commands. Today only `azd ai agent doctor` returns `2`, meaning all checks were skipped (e.g. preconditions unmet). |

## Running Local Agents

`azd ai agent run` starts the selected agent locally and, by default, opens the
Agent Inspector after the local agent port is accepting connections. The
inspector launch is best-effort: if `azure.ai.inspector` is not installed or
fails to start, the agent process keeps running and azd prints install guidance.

Use `--no-inspector` to run only the local agent process:

```bash
azd ai agent run --no-inspector
```

## Local Development

### Prerequisites

1. **Install developer kit extension** (if not already installed):
   ```bash
   azd ext install microsoft.azd.extensions
   ```

   > **Note**: If you encounter an error about the extension not being in the registry, verify you have the default source configured:
   > ```bash
   > azd ext source list
   > ```
   > If missing, add it:
   > ```bash
   > azd ext source add -n azd -t url -l "https://aka.ms/azd/extensions/registry"
   > ```

### Building and Installing

1. **Navigate to the extension directory**:
   ```bash
   cd cli/azd/extensions/azure.ai.agents
   ```

2. **Initial setup** (first time only):
   ```bash
   azd x build
   azd x pack
   azd x publish
   ```

3. **Install the extension**:
   ```bash
   azd ext install azure.ai.agents
   ```

4. **For subsequent development** (after initial setup):
   ```bash
   azd x watch
   ```
   This automatically watches for file changes, rebuilds, and installs updates locally.

   Or for manual builds:
   ```bash
   azd x build
   ```
   This builds and automatically installs the updated extension.

> [!NOTE]
> The `pack` and `publish` steps are only required for the first time setup. For ongoing development, `azd x watch` or `azd x build` handles all updates automatically.
