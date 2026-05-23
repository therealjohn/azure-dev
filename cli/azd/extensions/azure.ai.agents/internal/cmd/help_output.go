// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// help_output.go layers three sections on top of the default cobra `--help`
// output:
//
//   1. A state-aware "Get started" preamble (root command only). Renders only
//      when the current workspace is incomplete -- quiet for fully-deployed
//      projects so seasoned users see no noise.
//
//   2. An ENVIRONMENT VARIABLES section. Documents how azd loads env vars
//      from .azure/<env>/.env and lists the agents-specific vars.
//
//   3. A DOCS & AGENT SKILLS section. Phase 1D points at commands that exist
//      today (show, project show, doctor). Phase 2 will add `azd ai agent
//      docs` references; Phase 3 will switch to `azd ai doc agent`.
//
// All three sections live in this file (not in banner.go) because banner.go
// is responsible only for the visual ASCII banner, and rendering decisions
// for the env-var/docs/preamble sections require a context-bound lookup
// that the banner doesn't need.

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// installAgentsHelpOutput installs the agents-extension help func: banner +
// state-aware preamble + default help body + env-vars + docs sections. Other
// subcommands' --help is unaffected.
func installAgentsHelpOutput(rootCmd *cobra.Command) {
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		w := cmd.OutOrStdout()
		if cmd == rootCmd {
			printBanner(w)
			if preamble := resolveGetStartedPreamble(cmd.Context()); preamble != "" {
				fmt.Fprintln(w, preamble)
				fmt.Fprintln(w)
			}
		}
		defaultHelp(cmd, args)
		if cmd == rootCmd {
			fmt.Fprintln(w)
			fmt.Fprint(w, environmentVariablesSection())
			fmt.Fprintln(w)
			fmt.Fprint(w, docsAndAgentSkillsSection())
		}
	})
}

// resolveGetStartedPreamble returns a short "Get started" hint when the
// current workspace is missing something the agent needs. Returns empty
// when nothing actionable is missing (fully deployed) so the help output
// stays terse for users who already know what they're doing.
//
// Detection ladder, top match wins:
//  1. No azure.yaml in cwd / parent     -> azd init + azd ai agent init
//  2. azure.yaml exists, no ai.agent svc -> azd ai agent init
//  3. ai.agent service, no project endpt -> azd provision + project show
//  4. Project endpoint, no AGENT_*_*_ENDPOINT env var -> azd deploy
//  5. Fully deployed                    -> empty
func resolveGetStartedPreamble(ctx context.Context) string {
	// Walk up the filesystem looking for azure.yaml. Best-effort -- any
	// error short-circuits to "no project detected" so the preamble can
	// still surface useful guidance.
	azureYamlPath, found := findAzureYaml()
	if !found {
		return formatGetStarted(
			"No azd project detected. Get started with:",
			"azd init                  Set up a new azd project in this directory.",
			"azd ai agent init         Initialize an azd ai agent project.",
		)
	}

	// Re-use the azd host to inspect the project. If the host isn't running
	// (e.g. someone invoked the extension binary directly), skip the deeper
	// detection -- the user already has azure.yaml, which is enough context
	// to call init or deploy themselves.
	azdClient, err := azdext.NewAzdClient()
	if err != nil {
		return ""
	}
	defer azdClient.Close()

	hasAgentSvc := hasAgentService(ctx, azdClient)
	if !hasAgentSvc {
		return formatGetStarted(
			fmt.Sprintf("azure.yaml at %s has no azd ai agent service. Get started with:", azureYamlPath),
			"azd ai agent init         Add an azd ai agent service to this project.",
		)
	}

	if !hasResolvedProjectEndpoint(ctx) {
		return formatGetStarted(
			"No Foundry project endpoint resolved. Get started with:",
			"azd provision             Provision Foundry resources for this project.",
			"azd ai agent project show Inspect the current project context.",
		)
	}

	if !hasDeployedAgent(ctx, azdClient) {
		return formatGetStarted(
			"Agent not yet deployed. Get started with:",
			"azd deploy                Deploy the agent.",
			"azd ai agent show         Inspect the deployed agent status (returns 'not_deployed' until then).",
		)
	}

	// Fully deployed -- stay quiet.
	return ""
}

// formatGetStarted renders the preamble block: a bold header line followed
// by two-column lines of `command  description`. Uses a clean two-column
// spacing style; the heading uses the same purple as the banner for visual unity.
func formatGetStarted(header string, lines ...string) string {
	var b strings.Builder
	purple := color.RGB(109, 53, 255).Add(color.Bold)
	b.WriteString(purple.Sprint(header))
	b.WriteString("\n\n")
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// findAzureYaml walks up from the current working directory looking for an
// azure.yaml. Returns the absolute path and true if found, empty + false
// otherwise. Bounded by the filesystem root.
func findAzureYaml() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, "azure.yaml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the root
			return "", false
		}
		dir = parent
	}
}

// hasAgentService reports whether the active azd project lists any service
// with type "azure.ai.agent". Best-effort -- returns false on any RPC or
// inspection error.
func hasAgentService(ctx context.Context, azdClient *azdext.AzdClient) bool {
	resp, err := azdClient.Project().Get(ctx, &azdext.EmptyRequest{})
	if err != nil || resp == nil || resp.Project == nil {
		return false
	}
	for _, svc := range resp.Project.Services {
		if svc != nil && strings.EqualFold(svc.Host, agentServiceHostName) {
			return true
		}
	}
	return false
}

// agentServiceHostName is the azure.yaml `host:` value for an azd ai agent
// service. Lower-case because the EqualFold comparison normalizes.
const agentServiceHostName = "azure.ai.agent"

// hasResolvedProjectEndpoint returns true when the 5-level cascade in
// resolveProjectEndpoint produces a value. Wraps the existing resolver so
// we don't replicate its precedence rules here.
func hasResolvedProjectEndpoint(ctx context.Context) bool {
	resolved, err := resolveProjectEndpoint(ctx, resolveProjectEndpointOpts{})
	return err == nil && resolved != nil && resolved.Endpoint != ""
}

// hasDeployedAgent returns true when ANY env value matching the pattern
// AGENT_*_ENDPOINT (or AGENT_*_*_ENDPOINT) exists on the current azd env.
// Best-effort -- treats RPC failures as "no deployed agent".
func hasDeployedAgent(ctx context.Context, azdClient *azdext.AzdClient) bool {
	envResp, err := azdClient.Environment().GetCurrent(ctx, &azdext.EmptyRequest{})
	if err != nil || envResp == nil || envResp.Environment == nil {
		return false
	}
	values, err := azdClient.Environment().GetValues(ctx, &azdext.GetEnvironmentRequest{
		Name: envResp.Environment.Name,
	})
	if err != nil || values == nil {
		return false
	}
	for _, kv := range values.KeyValues {
		if kv == nil {
			continue
		}
		if strings.HasPrefix(kv.Key, "AGENT_") && strings.HasSuffix(kv.Key, "_ENDPOINT") && kv.Value != "" {
			return true
		}
	}
	return false
}

// environmentVariablesSection renders the ENVIRONMENT VARIABLES help block.
// Documents the .azure/<env>/.env mechanism plus the agent-specific vars.
// Lives on the root --help only so it stays terse on leaf-command help.
func environmentVariablesSection() string {
	var b strings.Builder
	bold := color.New(color.Bold)
	b.WriteString(bold.Sprint("ENVIRONMENT VARIABLES"))
	b.WriteString("\n  azd loads environment variables from `.azure/<env-name>/.env` in your\n")
	b.WriteString("  project. Manage them with:\n\n")
	b.WriteString("    azd env list                  List azd environments in this project.\n")
	b.WriteString("    azd env new <name>            Create a new azd environment.\n")
	b.WriteString("    azd env select <name>         Switch the active azd environment.\n")
	b.WriteString("    azd env get <KEY>             Read a value from the active env.\n")
	b.WriteString("    azd env set <KEY> <VALUE>     Write a value to the active env.\n\n")
	b.WriteString("  Variables read by this extension:\n\n")
	b.WriteString("    AZURE_AI_PROJECT_ENDPOINT     Project endpoint, read from active azd env.\n")
	b.WriteString("    FOUNDRY_PROJECT_ENDPOINT      Host-shell fallback when no azd env value.\n")
	b.WriteString("    AZURE_AI_PROJECT_ID           ARM resource ID; used to build the Foundry\n")
	b.WriteString("                                  portal playground URL.\n")
	b.WriteString("    AGENT_<SVC>_<PROTO>_ENDPOINT  Per-service deployed endpoint URL, one per\n")
	b.WriteString("                                  protocol (e.g. AGENT_MYAGENT_RESPONSES_ENDPOINT).\n")
	b.WriteString("    AGENT_<SVC>_ENDPOINT          Legacy single-endpoint var for older deployments.\n")
	b.WriteString("    AI_AGENT_PENDING_PROVISION    Internal: resources awaiting provisioning so the\n")
	b.WriteString("                                  resolver can surface accurate next-step guidance.\n")
	return b.String()
}

// docsAndAgentSkillsSection renders the DOCS & AGENT SKILLS help block.
// Phase 1D + Phase 2 + Phase 3: lists the agent-friendly read paths,
// the in-binary `azd ai agent docs` topic surface, and the unified
// front-door `azd ai doc agent` command from the azure.ai.docs extension.
//
// When azure.ai.docs is not installed, `azd ai doc agent` will fail with
// an install hint; that's intentional -- we want users to know the
// preferred entry point even if they haven't installed the docs ext yet.
func docsAndAgentSkillsSection() string {
	var b strings.Builder
	bold := color.New(color.Bold)
	b.WriteString(bold.Sprint("DOCS & AGENT SKILLS"))
	b.WriteString("\n  Inspect state, identity, and health from the terminal:\n\n")
	b.WriteString("    azd ai agent show --output json                Inspect the deployed agent record (JSON).\n")
	b.WriteString("    azd ai agent project show --output json        Inspect identity, subscription, and project context.\n")
	b.WriteString("    azd ai agent doctor --output json              Diagnose configuration, auth, and deployment issues.\n")
	b.WriteString("\n  Agent-friendly workflow docs (markdown, embedded in this binary):\n\n")
	b.WriteString("    azd ai agent docs                              List available skill topics.\n")
	b.WriteString("    azd ai agent docs --topic <name>               Print one of: initialize, configure, investigate, operate.\n")
	b.WriteString("\n  Unified front door across every azure.ai.* extension (requires azure.ai.docs):\n\n")
	b.WriteString("    azd ai doc                                     List ai.* extensions with docs available.\n")
	b.WriteString("    azd ai doc agent                               List skill topics for this extension.\n")
	b.WriteString("    azd ai doc agent <topic>                       Print one topic via the docs front door.\n")
	return b.String()
}
