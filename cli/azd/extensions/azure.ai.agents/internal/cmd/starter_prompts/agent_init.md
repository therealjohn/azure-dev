Initialize a Microsoft Foundry agent in this project at {{.ProjectPath}}.

Use the AZD AI skill{{if .SkillPath}} (installed at {{.SkillPath}}){{end}} to drive
the end-to-end flow. Pull deeper guidance for any step BEFORE running write
commands -- topics are named after the verbs of the journey:

```bash
azd ai doc agent              # list all topics
azd ai doc agent <topic>      # samples | initialize | develop | configure |
                              # extend | deploy | evaluate | operate | investigate
```

## The journey

1. **Confirm context.** Always start here.
   - `azd ai agent project show --output json` -- identity + subscription + endpoint.
   - `azd ai agent show --output json` -- branch on `.status` and follow the
     `next_step.suggestions[]` it returns.

2. **`initialize`** -- scaffold the agent. THIS directory already contains the
   AZD AI bootstrap stub (`azure.yaml` with `metadata.template: azd-ai-bootstrap@*`),
   so `azd ai agent init` auto-routes through the from-code path. Do NOT pass
   `--from-code` -- that flag is reserved for directories that already contain
   the user's agent code.

   ```bash
   azd ai agent init --no-prompt \
     --project-id "<projectResourceId>" \
     --deploy-mode code \
     --runtime python_3_13 \
     --entry-point app.py
   ```

3. **`deploy`** (part 1) -- `azd provision --no-prompt` to create Foundry
   resources.

4. **`develop`** -- before pushing to Foundry, validate locally:
   - `azd ai agent run` (starts the local agent + opens Agent Inspector)
   - `azd ai agent invoke --local "smoke test"` (no billing, no envelope)

5. **`deploy`** (part 2) -- `azd deploy --no-prompt` registers a new immutable
   agent version on the Foundry project.

6. **`investigate`** + **`operate`** -- verify the deployed agent:
   - `azd ai agent show --output json` (expect status `active`/`deployed`)
   - `azd ai agent invoke "smoke test"` (gated by the confirmation envelope)
   - `azd ai agent monitor --follow` to stream logs while you exercise it

## Ground rules

* Use `--output json` and `--no-prompt` wherever possible so the output stays
  machine-parseable and you never block on prompts.
* When any command exits with code 2 and a `confirmation_required` envelope,
  summarize `changes[]` in plain English and wait for my approval before
  re-running the printed `confirmCommand` with `--force`.
* Ask me before:
  - Picking a `--project-id` if I have not given you one.
  - Picking a model deployment when several options exist.
  - Any destructive operation that returned a confirmation envelope.
* For schema-level edits (`agent.yaml`: model, tools, connections, protocols,
  endpoint/card), read `azd ai doc agent extend` first and show me a diff
  before writing.
