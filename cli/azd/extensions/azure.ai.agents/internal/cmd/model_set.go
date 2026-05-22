// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"azureaiagent/internal/exterrors"
	"azureaiagent/internal/pkg/agents/agent_yaml"
	"azureaiagent/internal/project"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// modelSetFlags holds the flags accepted by `azd ai agent model set`.
type modelSetFlags struct {
	agentName  string
	version    string
	sku        string
	capacity   int
	format     string
	versionSet bool // true when --version was explicitly provided
}

// newModelCommand builds the `azd ai agent model` command group. It holds
// subcommands for managing the model deployments referenced by a hosted
// agent service.
func newModelCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	extCtx = ensureExtensionContext(extCtx)

	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage model deployments for a hosted agent.",
		Long: `Manage model deployments for a hosted agent service.

Use these commands to update the deployment metadata (version, SKU,
capacity, format) that azd will provision for a model resource declared
in an agent.yaml. This is the headless complement to ` +
			"`azd ai agent init`" + ` for coding agents and CI
pipelines that cannot run init interactively.`,
	}

	cmd.AddCommand(newModelSetCommand(extCtx))

	return cmd
}

// newModelSetCommand builds the `azd ai agent model set <model>` subcommand.
func newModelSetCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	flags := &modelSetFlags{}
	action := &ModelSetAction{flags: flags}
	extCtx = ensureExtensionContext(extCtx)

	cmd := &cobra.Command{
		Use:   "set <model>",
		Short: "Set or update the model deployment for a hosted agent.",
		Long: `Set or update the model deployment for a hosted agent service.

Updates the matching model resource in agent.yaml and the corresponding
deployments[] entry in azure.yaml. The command is fully non-interactive
and is intended for use after ` + "`azd ai agent init --no-prompt`" + `
when subscription/location were not yet set.

The positional <model> must match the 'id' field of an existing
model resource in agent.yaml. To add a new model resource, re-run
` + "`azd ai agent init`" + `.

Version resolution order:
  1. --version flag (used verbatim).
  2. Azure model catalog lookup (when AZURE_SUBSCRIPTION_ID and
     AZURE_LOCATION are set in the azd environment).
  3. Otherwise, the command fails with a validation error.

The command is idempotent: re-running with the same flags is a no-op.`,
		Example: `  # Set the version explicitly (no Azure calls)
  azd ai agent model set gpt-4o-mini --agent-name my-agent --version 2024-07-18

  # Let the catalog resolve the version (requires AZURE_SUBSCRIPTION_ID/AZURE_LOCATION)
  azd env set AZURE_SUBSCRIPTION_ID <sub>
  azd env set AZURE_LOCATION eastus2
  azd ai agent model set gpt-4o-mini --agent-name my-agent

  # Override SKU and capacity
  azd ai agent model set gpt-4o-mini --agent-name my-agent --sku DataZoneStandard --capacity 50`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.versionSet = cmd.Flags().Changed("version")
			ctx := azdext.WithAccessToken(cmd.Context())
			action.modelID = args[0]
			action.noPrompt = extCtx.NoPrompt
			return action.Run(ctx)
		},
	}

	cmd.Flags().StringVarP(
		&flags.agentName, "agent-name", "n", "",
		"Agent name (matches azure.yaml service name; "+
			"required when multiple azure.ai.agent services exist)",
	)
	cmd.Flags().StringVar(
		&flags.version, "version", "",
		"Model version (e.g. 2024-07-18). When omitted, the version is "+
			"resolved from the Azure model catalog using the azd environment's "+
			"AZURE_SUBSCRIPTION_ID and AZURE_LOCATION.",
	)
	cmd.Flags().StringVar(
		&flags.sku, "sku", "",
		"Deployment SKU name. Defaults to "+defaultDeploymentSku+
			" when neither the flag nor agent.yaml specifies one.",
	)
	cmd.Flags().IntVar(
		&flags.capacity, "capacity", 0,
		"Deployment capacity. Defaults to "+strconv.Itoa(defaultDeploymentCapacity)+
			" when neither the flag nor agent.yaml specifies one.",
	)
	cmd.Flags().StringVar(
		&flags.format, "format", "",
		"Model format. Defaults to "+defaultDeploymentFormat+
			" when neither the flag nor agent.yaml specifies one.",
	)

	return cmd
}

// ModelSetAction implements the `azd ai agent model set` subcommand.
type ModelSetAction struct {
	flags    *modelSetFlags
	modelID  string
	noPrompt bool
}

// Run executes the model set command end-to-end.
//
// High-level flow:
//  1. Validate flag combinations (rejects --version "" explicitly).
//  2. Open the azd client and resolve the agent service from azure.yaml.
//  3. Load agent.yaml and locate the matching ModelResource by Id.
//  4. Merge flag overrides into the resource (preserves the resource Name).
//  5. Resolve the deployment version (flag > catalog > error).
//  6. Apply constants for any remaining empty Format/Sku/Capacity fields.
//  7. Compute the target project.Deployment and detect a no-op (idempotent).
//  8. Write agent.yaml when the resource changed.
//  9. Upsert the service in azure.yaml when the deployment changed.
func (a *ModelSetAction) Run(ctx context.Context) error {
	if err := a.validateFlags(); err != nil {
		return err
	}

	azdClient, err := azdext.NewAzdClient()
	if err != nil {
		return exterrors.Internal(
			exterrors.CodeAzdClientFailed,
			fmt.Sprintf("failed to create azd client: %s", err),
		)
	}
	defer azdClient.Close()

	svc, proj, err := resolveAgentService(ctx, azdClient, a.flags.agentName, a.noPrompt)
	if err != nil {
		return err
	}

	envName, err := currentEnvironmentName(ctx, azdClient)
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(proj.Path, svc.RelativePath, "agent.yaml")
	manifestBytes, err := os.ReadFile(manifestPath) //nolint:gosec // G304: path constructed from azd project root + service config
	if err != nil {
		return exterrors.Dependency(
			exterrors.CodeFileNotFound,
			fmt.Sprintf("failed to read agent.yaml at %q: %s", manifestPath, err),
			"Verify the agent service's relativePath in azure.yaml points to a directory "+
				"containing agent.yaml. Re-run 'azd ai agent init' if the agent has not been scaffolded yet.",
		)
	}

	manifest, err := agent_yaml.LoadAndValidateAgentManifest(manifestBytes)
	if err != nil {
		return err
	}

	modelIdx, currentResource, err := findModelResource(manifest, a.modelID, svc.Name)
	if err != nil {
		return err
	}

	updatedResource := a.applyFlagOverrides(currentResource)

	if updatedResource.Version == "" {
		if format, version, ok := a.tryCatalogResolve(ctx, azdClient, envName); ok {
			if updatedResource.Format == "" {
				updatedResource.Format = format
			}
			updatedResource.Version = version
		}
	}

	if updatedResource.Version == "" {
		return exterrors.Validation(
			exterrors.CodeModelVersionRequired,
			fmt.Sprintf("a model version is required to set model '%s' for agent '%s'", a.modelID, svc.Name),
			"Either pass --version explicitly (for example: --version 2024-07-18), "+
				"or set AZURE_SUBSCRIPTION_ID and AZURE_LOCATION in the azd environment "+
				"with 'azd env set' so the version can be resolved from the Azure model catalog.",
		)
	}

	if updatedResource.Format == "" {
		updatedResource.Format = defaultDeploymentFormat
	}
	if updatedResource.Sku == "" {
		updatedResource.Sku = defaultDeploymentSku
	}
	if updatedResource.Capacity == 0 {
		updatedResource.Capacity = defaultDeploymentCapacity
	}

	targetDeployment := deploymentFromResource(updatedResource)

	var agentConfig project.ServiceTargetAgentConfig
	if err := project.UnmarshalStruct(svc.Config, &agentConfig); err != nil {
		return exterrors.Internal(
			exterrors.CodeInvalidServiceConfig,
			fmt.Sprintf("failed to read service config for agent '%s': %s", svc.Name, err),
		)
	}

	deployIdx := indexOfDeployment(agentConfig.Deployments, targetDeployment.Name)

	resourceUnchanged := currentResource == updatedResource
	deploymentUnchanged := deployIdx >= 0 && agentConfig.Deployments[deployIdx] == targetDeployment

	if resourceUnchanged && deploymentUnchanged {
		fmt.Println(output.WithSuccessFormat(
			"Model deployment for agent '%s' is already up to date.", svc.Name,
		))
		return nil
	}

	if !resourceUnchanged {
		manifest.Resources[modelIdx] = updatedResource
		newManifestBytes, err := yaml.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("failed to marshal updated agent manifest: %w", err)
		}
		if err := os.WriteFile(manifestPath, newManifestBytes, 0600); err != nil {
			return fmt.Errorf("failed to write agent.yaml at %q: %w", manifestPath, err)
		}
	}

	if !deploymentUnchanged {
		if deployIdx >= 0 {
			agentConfig.Deployments[deployIdx] = targetDeployment
		} else {
			agentConfig.Deployments = append(agentConfig.Deployments, targetDeployment)
		}

		updatedConfig, err := project.MarshalStruct(&agentConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal updated service config: %w", err)
		}
		svc.Config = updatedConfig

		if _, err := azdClient.Project().AddService(ctx, &azdext.AddServiceRequest{Service: svc}); err != nil {
			return fmt.Errorf("failed to update service '%s' in azure.yaml: %w", svc.Name, err)
		}
	}

	fmt.Println(output.WithSuccessFormat(
		"Updated model deployment for agent '%s': %s version %s (sku=%s, capacity=%d).",
		svc.Name, updatedResource.Id, updatedResource.Version, updatedResource.Sku, updatedResource.Capacity,
	))

	return nil
}

// validateFlags rejects empty/whitespace --version (when the flag was set
// explicitly) and negative --capacity.
func (a *ModelSetAction) validateFlags() error {
	if a.flags.versionSet && strings.TrimSpace(a.flags.version) == "" {
		return exterrors.Validation(
			exterrors.CodeInvalidParameter,
			"--version cannot be empty",
			"Provide a non-empty version (for example: --version 2024-07-18), "+
				"or omit --version to resolve the version from the Azure model catalog.",
		)
	}
	if a.flags.capacity < 0 {
		return exterrors.Validation(
			exterrors.CodeInvalidParameter,
			fmt.Sprintf("--capacity must be a positive integer (got %d)", a.flags.capacity),
			"Pass --capacity with a value greater than 0, or omit the flag to use the default.",
		)
	}
	return nil
}

// applyFlagOverrides returns a copy of resource with non-empty flag values applied.
// Resource.Name and Resource.Kind are always preserved -- the binding key into the
// agent template never changes via this subcommand.
func (a *ModelSetAction) applyFlagOverrides(resource agent_yaml.ModelResource) agent_yaml.ModelResource {
	out := resource
	if a.flags.versionSet {
		out.Version = strings.TrimSpace(a.flags.version)
	}
	if a.flags.sku != "" {
		out.Sku = a.flags.sku
	}
	if a.flags.capacity > 0 {
		out.Capacity = a.flags.capacity
	}
	if a.flags.format != "" {
		out.Format = a.flags.format
	}
	return out
}

// tryCatalogResolve looks up the model in the Azure model catalog when
// the azd environment has both AZURE_SUBSCRIPTION_ID and AZURE_LOCATION set.
// Returns ok=false on any missing value or call failure -- callers fall
// through to the "version required" error path.
func (a *ModelSetAction) tryCatalogResolve(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	envName string,
) (format string, version string, ok bool) {
	sub, err := getEnvValue(ctx, azdClient, envName, "AZURE_SUBSCRIPTION_ID")
	if err != nil || sub == "" {
		return "", "", false
	}
	loc, err := getEnvValue(ctx, azdClient, envName, "AZURE_LOCATION")
	if err != nil || loc == "" {
		return "", "", false
	}

	azureContext := &azdext.AzureContext{
		Scope: &azdext.AzureScope{
			SubscriptionId: sub,
			Location:       loc,
		},
	}

	return lookupCatalogModel(ctx, azdClient, azureContext, a.modelID)
}

// findModelResource returns the index and value of the ModelResource whose
// Id matches modelID. Returns a structured Validation error when not found.
func findModelResource(
	manifest *agent_yaml.AgentManifest,
	modelID string,
	agentName string,
) (int, agent_yaml.ModelResource, error) {
	for i, r := range manifest.Resources {
		if m, ok := r.(agent_yaml.ModelResource); ok && m.Id == modelID {
			return i, m, nil
		}
	}
	return -1, agent_yaml.ModelResource{}, exterrors.Validation(
		exterrors.CodeModelResourceNotFound,
		fmt.Sprintf("agent.yaml for agent '%s' has no model resource with id '%s'", agentName, modelID),
		"Verify the model id matches an existing 'kind: model' resource in agent.yaml. "+
			"To add a new model resource, re-run 'azd ai agent init' or edit agent.yaml directly.",
	)
}

// deploymentFromResource builds a project.Deployment using the resource Id as
// the deployment Name. Matches the convention used by buildHeadlessDeployment
// (and the legacy interactive flow) so model set is idempotent against an
// existing azure.yaml.
func deploymentFromResource(r agent_yaml.ModelResource) project.Deployment {
	return project.Deployment{
		Name: r.Id,
		Model: project.DeploymentModel{
			Name:    r.Id,
			Format:  r.Format,
			Version: r.Version,
		},
		Sku: project.DeploymentSku{
			Name:     r.Sku,
			Capacity: r.Capacity,
		},
	}
}

// indexOfDeployment returns the index of the deployment with the given name,
// or -1 when no match exists.
func indexOfDeployment(deployments []project.Deployment, name string) int {
	for i, d := range deployments {
		if d.Name == name {
			return i
		}
	}
	return -1
}

// currentEnvironmentName fetches the name of the currently active azd environment.
func currentEnvironmentName(ctx context.Context, azdClient *azdext.AzdClient) (string, error) {
	envResp, err := azdClient.Environment().GetCurrent(ctx, &azdext.EmptyRequest{})
	if err != nil {
		return "", exterrors.Dependency(
			exterrors.CodeEnvironmentNotFound,
			fmt.Sprintf("failed to resolve the current azd environment: %s", err),
			"Run 'azd env new <name>' or 'azd env select <name>' to set an active environment, "+
				"then re-run this command.",
		)
	}
	if envResp == nil || envResp.Environment == nil || envResp.Environment.Name == "" {
		return "", exterrors.Dependency(
			exterrors.CodeEnvironmentNotFound,
			"no active azd environment is set",
			"Run 'azd env new <name>' or 'azd env select <name>' to set an active environment, "+
				"then re-run this command.",
		)
	}
	return envResp.Environment.Name, nil
}

// lookupCatalogModel queries the Azure model catalog via gRPC for the given
// model name and returns the canonical Format and default Version. Returns
// ok=false on any failure (network, model not found, no versions available).
//
// This is the headless equivalent of (*InitAction).tryLookupCatalogModel:
// no spinner, no prompts, safe in --no-prompt mode.
func lookupCatalogModel(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	azureContext *azdext.AzureContext,
	modelName string,
) (format string, version string, ok bool) {
	modelResp, err := azdClient.Ai().ListModels(ctx, &azdext.ListModelsRequest{
		AzureContext: azureContext,
		Filter:       agentModelFilter(nil, nil),
	})
	if err != nil || modelResp == nil {
		return "", "", false
	}

	var model *azdext.AiModel
	for _, m := range modelResp.Models {
		if m != nil && m.Name == modelName {
			model = m
			break
		}
	}
	if model == nil {
		return "", "", false
	}

	chosen := ""
	for _, v := range model.Versions {
		if v != nil && v.IsDefault {
			chosen = v.Version
			break
		}
	}
	if chosen == "" {
		for _, v := range model.Versions {
			if v != nil && v.Version != "" {
				chosen = v.Version
				break
			}
		}
	}
	if chosen == "" {
		return "", "", false
	}

	return model.Format, chosen, true
}
