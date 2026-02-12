// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

// show.go implements the `azd ai agent show` command.
//
// This is a standalone CLI command that displays comprehensive information about
// a deployed agent, including endpoints, portal links, container status (for hosted
// agents), and agent metadata. It is the extension equivalent of
// `az cognitiveservices agent show`, enriched with application endpoint URLs and
// portal links that azd users expect.

import (
	"context"
	"encoding/json"
	"fmt"

	"azureaiagent/internal/pkg/agents/agent_api"

	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// showFlags holds the command-line flags for the show command.
type showFlags struct {
	service string
}

func newShowCommand() *cobra.Command {
	flags := &showFlags{}

	cmd := &cobra.Command{
		Use:   "show",
		Short: fmt.Sprintf("Show details of a deployed agent. %s", color.YellowString("(Preview)")),
		Long: `Display comprehensive information about a deployed agent including:
  - Portal playground link
  - Agent and application endpoints
  - Container status (for hosted agents)
  - Agent metadata (kind, description, tools, model)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(flags)
		},
	}

	cmd.Flags().StringVar(
		&flags.service,
		"service",
		"",
		"The name of the agent service in azure.yaml. Defaults to the first agent service.",
	)

	return cmd
}

func runShow(flags *showFlags) error {
	// Load agent context by reading azure.yaml and .azure/<env>/.env directly.
	// Standalone commands cannot use the azd gRPC client (see env_helpers.go).
	agentCtx, err := loadAgentContext(flags.service)
	if err != nil {
		return err
	}

	fmt.Println()
	headerColor := color.New(color.FgCyan, color.Bold)

	// --- Fetch agent details from REST API first ---
	// We fetch early so we can determine the agent kind (hosted vs prompt)
	// and display container status before other sections.
	// This provides the same data as `az cognitiveservices agent show`.
	const agentAPIVersion = "2025-05-15-preview"
	agentClient := agent_api.NewAgentClient(agentCtx.ProjectEndpoint, agentCtx.Credential)

	agent, apiErr := agentClient.GetAgent(context.Background(), agentCtx.AgentName, agentAPIVersion)

	var agentKind string
	if apiErr == nil {
		agentKind = extractAgentKind(agent.Versions.Latest.Definition)
	}

	// --- Section 1: Container Status (hosted agents only) ---
	// Shown first because container health is the most urgent information
	// when debugging a hosted agent deployment.
	if apiErr == nil && agentKind == string(agent_api.AgentKindHosted) {
		container, err := agentClient.GetAgentContainer(
			context.Background(), agentCtx.AgentName, agentCtx.AgentVersion, agentAPIVersion,
		)
		if err == nil {
			headerColor.Println("Container Status")
			fmt.Println(color.HiBlackString("================"))
			fmt.Printf("  Status:       %s\n", colorizeStatus(string(container.Status)))
			if container.MinReplicas != nil {
				fmt.Printf("  Min Replicas: %d\n", *container.MinReplicas)
			}
			if container.MaxReplicas != nil {
				fmt.Printf("  Max Replicas: %d\n", *container.MaxReplicas)
			}
			if container.CreatedAt != "" {
				fmt.Printf("  Created:      %s\n", container.CreatedAt)
			}
			if container.UpdatedAt != "" {
				fmt.Printf("  Updated:      %s\n", container.UpdatedAt)
			}
			if container.ErrorMessage != nil && *container.ErrorMessage != "" {
				fmt.Printf("  Error:        %s\n", color.RedString(*container.ErrorMessage))
			}
			fmt.Println()
		}
	}

	// --- Section 2: Agent Details ---
	if apiErr != nil {
		// If we can't reach the API, print what we know from env vars and warn
		fmt.Fprintf(
			color.Error,
			"%s Could not fetch agent details from API: %v\n",
			color.YellowString("WARNING:"),
			apiErr,
		)
	} else {
		headerColor.Println("Agent Details")
		fmt.Println(color.HiBlackString("============="))
		fmt.Printf("  Name:        %s\n", agent.Name)
		fmt.Printf("  ID:          %s\n", agent.ID)

		latestVersion := agent.Versions.Latest
		fmt.Printf("  Version:     %s\n", latestVersion.Version)

		if latestVersion.Description != nil && *latestVersion.Description != "" {
			fmt.Printf("  Description: %s\n", *latestVersion.Description)
		}

		if agentKind != "" {
			fmt.Printf("  Kind:        %s\n", agentKind)
		}

		// Display model info for prompt agents
		displayPromptAgentInfo(latestVersion.Definition)

		if len(latestVersion.Metadata) > 0 {
			fmt.Printf("  Metadata:    %v\n", latestVersion.Metadata)
		}

		fmt.Println()
	}

	// --- Section 3: Endpoints ---
	// Shown last so the user sees the most actionable URLs at the bottom
	// of the terminal output (closest to the cursor).
	headerColor.Println("Endpoints")
	fmt.Println(color.HiBlackString("========="))

	// Agent playground URL (portal link for the build/test page)
	if agentCtx.ProjectResourceID != "" {
		playgroundURL, err := buildPortalURL(agentCtx.ProjectResourceID, agentCtx.AgentName, agentCtx.AgentVersion, "build")
		if err == nil {
			fmt.Printf("  Agent playground (portal): %s\n", output.WithLinkFormat(playgroundURL))
		}
	}

	// Agent endpoint — the versioned agent API URL
	agentEndpoint := fmt.Sprintf("%s/agents/%s/versions/%s", agentCtx.ProjectEndpoint, agentCtx.AgentName, agentCtx.AgentVersion)
	fmt.Printf("  Agent endpoint:            %s\n", agentEndpoint)

	// Application endpoints — the protocol-specific URLs for invoking the agent.
	// These are constructed from the project endpoint using the "applications" path
	// and include the protocol (Responses API or Activity Protocol).
	applicationBaseURL := fmt.Sprintf("%s/applications/%s", agentCtx.ProjectEndpoint, agentCtx.AgentName)
	apiVersion := "2025-11-15-preview"

	responsesEndpoint := fmt.Sprintf(
		"%s/protocols/openai/responses?api-version=%s", applicationBaseURL, apiVersion,
	)
	fmt.Printf("  Application (Responses):   %s\n", responsesEndpoint)

	activityEndpoint := fmt.Sprintf(
		"%s/protocols/activityprotocol?api-version=%s", applicationBaseURL, apiVersion,
	)
	fmt.Printf("  Application (Activity):    %s\n", activityEndpoint)

	// Monitor page (portal link for the monitor/logging page)
	if agentCtx.ProjectResourceID != "" {
		monitorURL, err := buildPortalURL(agentCtx.ProjectResourceID, agentCtx.AgentName, agentCtx.AgentVersion, "monitor")
		if err == nil {
			fmt.Printf("  Monitor (portal):          %s\n", output.WithLinkFormat(monitorURL))
		}
	}

	fmt.Println()
	fmt.Printf("  For information on invoking the agent, see %s\n", output.WithLinkFormat("https://aka.ms/azd-agents-invoke"))
	fmt.Println()

	return nil
}

// extractAgentKind attempts to determine the agent kind (prompt, hosted, etc.)
// from the definition field, which is returned as an untyped interface{} from the API.
func extractAgentKind(definition interface{}) string {
	if definition == nil {
		return ""
	}

	// The API returns the definition as a JSON object; marshal/unmarshal to inspect it
	data, err := json.Marshal(definition)
	if err != nil {
		return ""
	}

	var defMap map[string]interface{}
	if err := json.Unmarshal(data, &defMap); err != nil {
		return ""
	}

	if kind, ok := defMap["kind"].(string); ok {
		return kind
	}
	return ""
}

// displayPromptAgentInfo prints model and instructions info if the agent is a prompt agent.
func displayPromptAgentInfo(definition interface{}) {
	if definition == nil {
		return
	}

	data, err := json.Marshal(definition)
	if err != nil {
		return
	}

	var defMap map[string]interface{}
	if err := json.Unmarshal(data, &defMap); err != nil {
		return
	}

	if model, ok := defMap["model"].(string); ok && model != "" {
		fmt.Printf("  Model:       %s\n", model)
	}

	if tools, ok := defMap["tools"].([]interface{}); ok && len(tools) > 0 {
		fmt.Printf("  Tools:       %d configured\n", len(tools))
	}
}

// colorizeStatus returns a color-coded string for container statuses
// to make it easy to visually identify running vs stopped vs failed states.
func colorizeStatus(status string) string {
	switch status {
	case "Running":
		return color.GreenString(status)
	case "Starting", "Updating":
		return color.YellowString(status)
	case "Failed":
		return color.RedString(status)
	case "Stopped", "Stopping", "Deleting", "Deleted":
		return color.HiBlackString(status)
	default:
		return status
	}
}
