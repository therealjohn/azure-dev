// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"fmt"
	"os"

	"azureaiagent/internal/pkg/agents/agent_api"
	"azureaiagent/internal/pkg/agents/agent_yaml"
	"azureaiagent/internal/pkg/paths"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/spf13/cobra"
	goyaml "go.yaml.in/yaml/v3"
)

func newEndpointCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Manage agent endpoint and card configuration.",
	}

	cmd.AddCommand(newEndpointUpdateCommand(extCtx))

	return cmd
}

type endpointUpdateFlags struct {
	name string
}

func newEndpointUpdateCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	flags := &endpointUpdateFlags{}
	gate := &confirmationGate{}
	extCtx = ensureExtensionContext(extCtx)

	cmd := &cobra.Command{
		Use:   "update [name]",
		Short: "Update an agent's endpoint and card configuration without deploying a new version.",
		Long: `Update an agent's endpoint and card configuration without deploying a new version.

This command reads the agent_endpoint and agent_card sections from agent.yaml and
patches the existing agent with those values. No new agent version is created.

The agent must already exist (i.e., it must have been previously deployed).

Confirmation: pass --force to skip the prompt (or the agent confirmation
envelope). Pass --dry-run to preview what would be patched without mutating.`,
		Example: `  # Update endpoint/card for the default agent service
  azd ai agent endpoint update

  # Update a specific agent service after confirming
  azd ai agent endpoint update my-agent --force

  # Preview without patching
  azd ai agent endpoint update my-agent --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.name = args[0]
			}

			ctx := azdext.WithAccessToken(cmd.Context())
			gate.noPrompt = extCtx.NoPrompt

			azdClient, err := azdext.NewAzdClient()
			if err != nil {
				return fmt.Errorf("failed to create azd client: %w", err)
			}
			defer azdClient.Close()

			return runEndpointUpdate(ctx, azdClient, flags, gate, extCtx)
		},
	}

	registerConfirmationFlags(cmd, gate)
	return cmd
}

func runEndpointUpdate(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	flags *endpointUpdateFlags,
	gate *confirmationGate,
	extCtx *azdext.ExtensionContext,
) error {
	// Resolve the agent service from the project.
	svc, proj, err := resolveAgentService(ctx, azdClient, flags.name, extCtx.NoPrompt)
	if err != nil {
		return err
	}

	// Read and parse agent.yaml.
	agentYamlPath, err := paths.JoinAllowRoot(proj.Path, svc.RelativePath, "agent.yaml")
	if err != nil {
		return fmt.Errorf("invalid agent.yaml path: %w", err)
	}
	data, err := os.ReadFile(agentYamlPath) //nolint:gosec // path validated by JoinAllowRoot
	if err != nil {
		return fmt.Errorf("failed to read agent.yaml: %w", err)
	}

	var agentDef agent_yaml.ContainerAgent
	if err := goyaml.Unmarshal(data, &agentDef); err != nil {
		return fmt.Errorf("failed to parse agent.yaml: %w", err)
	}

	// Validate that endpoint or card is defined.
	if agentDef.AgentEndpoint == nil && agentDef.AgentCard == nil {
		return fmt.Errorf(
			"agent.yaml for service %q does not define agent_endpoint or agent_card — nothing to update",
			svc.Name,
		)
	}

	// Confirmation gate -- describes the patch as concisely as possible.
	changes := []string{}
	if agentDef.AgentEndpoint != nil {
		changes = append(changes, fmt.Sprintf("Will patch agent_endpoint on agent %q", agentDef.Name))
	}
	if agentDef.AgentCard != nil {
		changes = append(changes, fmt.Sprintf("Will patch agent_card on agent %q", agentDef.Name))
	}
	confirmCmd := "azd ai agent endpoint update"
	if flags.name != "" {
		confirmCmd += " " + flags.name
	}
	confirmCmd += " --force"
	proceed, err := confirmWrite(ctx, ConfirmationRequest{
		CommandPath:    "agent endpoint update",
		Description:    fmt.Sprintf("Patch endpoint/card configuration for agent %q without redeploying.", agentDef.Name),
		Classification: ConfirmationClassification{Idempotent: true},
		Changes:        changes,
		ConfirmCommand: confirmCmd,
	}, *gate)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	// Map YAML endpoint/card fields to API models (skips full definition validation).
	apiEndpoint, apiCard, err := agent_yaml.MapEndpointAndCard(agentDef.AgentEndpoint, agentDef.AgentCard)
	if err != nil {
		return fmt.Errorf("failed to map endpoint/card fields: %w", err)
	}

	// Resolve endpoint and create client.
	agentContext, err := newAgentContext(ctx, "", "", agentDef.Name, "")
	if err != nil {
		return err
	}

	agentClient, err := agentContext.NewClient()
	if err != nil {
		return err
	}

	// Patch endpoint/card fields.
	patchRequest := &agent_api.PatchAgentRequest{
		AgentEndpoint: apiEndpoint,
		AgentCard:     apiCard,
	}

	_, err = agentClient.PatchAgent(ctx, agentDef.Name, patchRequest, DefaultAgentAPIVersion)
	if err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agentDef.Name, err)
	}

	fmt.Fprint(os.Stdout, output.WithSuccessFormat("Agent %q endpoint/card configuration updated successfully.\n", agentDef.Name))
	return nil
}
