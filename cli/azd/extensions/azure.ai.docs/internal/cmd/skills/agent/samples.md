---
short: Discover starter samples and templates before running init.
order: 5
---
# Samples: discover a starting point before scaffolding

Audience: an AI coding assistant choosing a starting point for a new agent
project on the user's behalf. Every command here is read-only and safe to
script.

The catalog this command surfaces is the SAME catalog the interactive
`azd ai agent init` picker uses. Each entry tells you exactly which command
to run next, so you do not need to compose `--manifest` URLs by hand.

----------------------------------------------------------------------

## When to use samples (vs. other init paths)

Three ways to start a project:

| Source                       | How to find it             | When to use                                            |
| ---------------------------- | -------------------------- | ------------------------------------------------------ |
| Curated sample (manifest)    | `azd ai agent sample list` | Fastest path; user has no opinion on starting code     |
| Curated template (full repo) | `azd ai agent sample list` | User wants an entire azd template scaffolded           |
| Local code in cwd            | `--from-code` flag         | Repo already contains the user's agent source          |

If the human asked for "something to get started", default to the sample
list rather than guessing a manifest URL.

----------------------------------------------------------------------

## List the catalog

```bash
azd ai agent sample list --output json
```

The catalog is fetched from the upstream registry every call (no local
cache), so this can take a second or two over a cold network.

Aliases: `sample ls`.

Filters (combine freely; all optional):

* `--featured-only` -- only the curated starter list (recommended default
  when you have no other signal from the human).
* `--language python` -- supported tokens: `python`, `dotnetCsharp`.
* `--type agent` -- only entries whose source is an `agent.yaml` manifest
  (ready for `azd ai agent init -m <url>`).
* `--type azd` -- only entries whose source is a full azd template
  repository (consumed by `azd init -t <url>`).

----------------------------------------------------------------------

## JSON shape

```json
{
  "templates": [
    {
      "title": "Echo agent (Python)",
      "description": "Minimal hosted echo agent. Good first-deploy smoke test.",
      "languages": ["python"],
      "type": "agent",
      "manifestUrl": "https://raw.githubusercontent.com/...",
      "repoUrl": "",
      "tags": ["featured", "agent", "python"],
      "featured": true,
      "recommended": false,
      "initCommand": "azd ai agent init -m \"https://raw.githubusercontent.com/.../agent.yaml\""
    }
  ]
}
```

Field contract:

* `type` is the discriminator. Switch on it instead of testing both URL
  fields for non-emptiness:
  * `"agent"` -- `manifestUrl` is set, `repoUrl` is empty. Pass to
    `azd ai agent init -m <manifestUrl>`.
  * `"azd"` -- `repoUrl` is set, `manifestUrl` is empty. Pass to
    `azd init -t <repoUrl>`; the agent extension runs afterwards once the
    full template is scaffolded.
* `initCommand` is a ready-to-execute string. The URL segment is shell-
  quoted, so it survives a copy/paste. Use this when emitting a command
  for the human; reach for `manifestUrl`/`repoUrl` when you want
  pre-tokenized argv.
* `featured` -- entry is in the curated starter list. Prefer these when
  picking automatically.
* `recommended` -- entry is the default pre-selected template in
  interactive mode. There is at most one of these per language.
* `languages` -- tokens match the values used by the interactive picker.
  Use them to filter when the human's project type is known.

Schema stability: fields added in future versions are additive. Ignore
unknown fields.

----------------------------------------------------------------------

## Pick a sample and scaffold

Single-shot, agent-friendly flow:

```bash
# 1. Get the catalog
azd ai agent sample list --featured-only --language python --output json

# 2. Pick an entry, then run its initCommand
azd ai agent init -m "<manifestUrl from step 1>" --no-prompt \
  --project-id "<projectResourceId>"
```

If the human has not given you a `--project-id`, stop and ask -- do not
guess. See the `initialize` topic for the full init contract.

For `type: "azd"` entries, the flow is two-step:

```bash
azd init -t "<repoUrl>"
cd <scaffolded-directory>
azd ai agent init --from-code   # or use the manifest the template ships
```

----------------------------------------------------------------------

## What this topic does NOT cover

* The full `azd ai agent init` flag set -- see `initialize`.
* `sample show <name>` -- this command does NOT exist today. The full
  catalog entry is already in the `sample list` JSON output.
* Building your own samples -- the catalog is curated upstream by the
  Foundry team.
