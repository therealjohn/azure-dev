Initialize a Microsoft Foundry agent in this project at {{.ProjectPath}}.

Use the Microsoft Foundry skill{{if .SkillPath}} (installed at {{.SkillPath}}){{end}} to drive the
end-to-end setup:

1. Verify identity and project context with `azd ai agent project show --output json`.
2. Decide whether to scaffold a new agent or pick up an existing one with
   `azd ai agent show --output json` and follow the `next_step.suggestions[]`.
3. Configure `agent.yaml` (model, instructions, tools) and show me a diff before
   editing.
4. Provision Foundry resources with `azd provision --no-prompt`.
5. Deploy with `azd deploy --no-prompt`.
6. Verify with `azd ai agent show --output json` and a smoke-test invocation
   via `azd ai agent invoke "hello"`.

Use `--output json` and `--no-prompt` wherever possible so the output stays
machine-parseable and you never block on prompts.

When any command exits with code 2 and a `confirmation_required` envelope,
summarize the `changes[]` for me in plain English and wait for my approval
before re-running with `--force`.

Ask me before:
- Picking a `--project-id` if I have not given you one.
- Picking a model deployment when several options exist.
- Any destructive operation that returned a confirmation envelope.
