// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"azureaiagent/internal/pkg/agents/agent_yaml"
	"azureaiagent/internal/pkg/azure"
	"azureaiagent/internal/project"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/braydonk/yaml"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"
)

func newListenCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "listen",
		Short:  "Starts the extension and listens for events.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create a new context that includes the AZD access token.
			ctx := azdext.WithAccessToken(cmd.Context())

			setupDebugLogging(cmd.Flags())

			// Create a new AZD client.
			azdClient, err := azdext.NewAzdClient()
			if err != nil {
				return fmt.Errorf("failed to create azd client: %w", err)
			}
			defer azdClient.Close()

			projectParser := &project.FoundryParser{AzdClient: azdClient}
			// IMPORTANT: service target name here must match the name used in the extension manifest.
			host := azdext.NewExtensionHost(azdClient).
				WithServiceTarget(AiAgentHost, func() azdext.ServiceTargetProvider {
					return project.NewAgentServiceTargetProvider(azdClient)
				}).
				WithProjectEventHandler("preprovision", func(ctx context.Context, args *azdext.ProjectEventArgs) error {
					return preprovisionHandler(ctx, azdClient, projectParser, args)
				}).
				WithProjectEventHandler("predeploy", func(ctx context.Context, args *azdext.ProjectEventArgs) error {
					return predeployHandler(ctx, azdClient, projectParser, args)
				}).
				WithProjectEventHandler("postdeploy", func(ctx context.Context, args *azdext.ProjectEventArgs) error {
					// Run existing Container App agent post-deploy handler
					if err := projectParser.CoboPostDeploy(ctx, args); err != nil {
						return err
					}
					// Run Application/A365 post-deploy handler for azure.ai.agent services
					return applicationPostDeployHandler(ctx, azdClient, args)
				})

			// Start listening for events
			// This is a blocking call and will not return until the server connection is closed.
			if err := host.Run(ctx); err != nil {
				return fmt.Errorf("failed to run extension: %w", err)
			}

			return nil
		},
	}
}

func preprovisionHandler(ctx context.Context, azdClient *azdext.AzdClient, projectParser *project.FoundryParser, args *azdext.ProjectEventArgs) error {
	if err := projectParser.SetIdentity(ctx, args); err != nil {
		return fmt.Errorf("failed to set identity: %w", err)
	}

	for _, svc := range args.Project.Services {
		switch svc.Host {
		case AiAgentHost:
			if err := populateContainerSettings(ctx, azdClient, svc); err != nil {
				return fmt.Errorf("failed to populate container settings for service %q: %w", svc.Name, err)
			}
			if err := envUpdate(ctx, azdClient, args.Project, svc); err != nil {
				return fmt.Errorf("failed to update environment for service %q: %w", svc.Name, err)
			}
		case ContainerAppHost:
			if err := containerAgentHandling(ctx, azdClient, args.Project, svc); err != nil {
				return fmt.Errorf("failed to handle container agent for service %q: %w", svc.Name, err)
			}
		}
	}

	return nil
}

func predeployHandler(ctx context.Context, azdClient *azdext.AzdClient, projectParser *project.FoundryParser, args *azdext.ProjectEventArgs) error {
	if err := projectParser.SetIdentity(ctx, args); err != nil {
		return fmt.Errorf("failed to set identity: %w", err)
	}

	for _, svc := range args.Project.Services {
		switch svc.Host {
		case AiAgentHost:
			if err := populateContainerSettings(ctx, azdClient, svc); err != nil {
				return fmt.Errorf("failed to populate container settings for service %q: %w", svc.Name, err)
			}
			if err := envUpdate(ctx, azdClient, args.Project, svc); err != nil {
				return fmt.Errorf("failed to update environment for service %q: %w", svc.Name, err)
			}
		}
	}

	return nil
}

func envUpdate(ctx context.Context, azdClient *azdext.AzdClient, azdProject *azdext.ProjectConfig, svc *azdext.ServiceConfig) error {

	var foundryAgentConfig *project.ServiceTargetAgentConfig

	if err := project.UnmarshalStruct(svc.Config, &foundryAgentConfig); err != nil {
		return fmt.Errorf("failed to parse foundry agent config: %w", err)
	}

	currentEnvResponse, err := azdClient.Environment().GetCurrent(ctx, &azdext.EmptyRequest{})
	if err != nil {
		return err
	}

	if err := kindEnvUpdate(ctx, azdClient, azdProject, svc, currentEnvResponse.Environment.Name); err != nil {
		return err
	}

	if len(foundryAgentConfig.Deployments) > 0 {
		if err := deploymentEnvUpdate(ctx, foundryAgentConfig.Deployments, azdClient, currentEnvResponse.Environment.Name); err != nil {
			return err
		}
	}

	if len(foundryAgentConfig.Resources) > 0 {
		if err := resourcesEnvUpdate(ctx, foundryAgentConfig.Resources, azdClient, currentEnvResponse.Environment.Name); err != nil {
			return err
		}
	}

	if foundryAgentConfig.Application != nil && foundryAgentConfig.Application.Enabled {
		if err := applicationEnvUpdate(ctx, foundryAgentConfig.Application, azdClient, currentEnvResponse.Environment.Name); err != nil {
			return err
		}
	}

	return nil
}

func kindEnvUpdate(ctx context.Context, azdClient *azdext.AzdClient, project *azdext.ProjectConfig, svc *azdext.ServiceConfig, envName string) error {
	servicePath := svc.RelativePath
	fullPath := filepath.Join(project.Path, servicePath)
	agentYamlPath := filepath.Join(fullPath, "agent.yaml")

	data, err := os.ReadFile(agentYamlPath)
	if err != nil {
		return fmt.Errorf("failed to read YAML file: %w", err)
	}

	err = agent_yaml.ValidateAgentDefinition(data)
	if err != nil {
		return fmt.Errorf("agent.yaml is not valid: %w", err)
	}

	var genericTemplate map[string]interface{}
	if err := yaml.Unmarshal(data, &genericTemplate); err != nil {
		return fmt.Errorf("YAML content is not valid: %w", err)
	}

	kind, ok := genericTemplate["kind"].(string)
	if !ok {
		return fmt.Errorf("kind field is not a valid string")
	}

	switch kind {
	case string(agent_yaml.AgentKindHosted):
		if err := setEnvVar(ctx, azdClient, envName, "ENABLE_HOSTED_AGENTS", "true"); err != nil {
			return err
		}
	}

	// Extract agent name from agent.yaml and set as AGENT_NAME for Bicep params
	if name, ok := genericTemplate["name"].(string); ok && name != "" {
		if err := setEnvVar(ctx, azdClient, envName, "AGENT_NAME", name); err != nil {
			return err
		}
		// Also default APPLICATION_NAME to the agent name if not already set
		if err := setEnvVarIfEmpty(ctx, azdClient, envName, "APPLICATION_NAME", name); err != nil {
			return err
		}
	}

	return nil
}

func deploymentEnvUpdate(ctx context.Context, deployments []project.Deployment, azdClient *azdext.AzdClient, envName string) error {
	deploymentsJson, err := json.Marshal(deployments)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment details to JSON: %w", err)
	}

	// Escape backslashes and double quotes for environment variable
	jsonString := string(deploymentsJson)
	escapedJsonString := strings.ReplaceAll(jsonString, "\\", "\\\\")
	escapedJsonString = strings.ReplaceAll(escapedJsonString, "\"", "\\\"")

	return setEnvVar(ctx, azdClient, envName, "AI_PROJECT_DEPLOYMENTS", escapedJsonString)
}

func resourcesEnvUpdate(ctx context.Context, resources []project.Resource, azdClient *azdext.AzdClient, envName string) error {
	resourcesJson, err := json.Marshal(resources)
	if err != nil {
		return fmt.Errorf("failed to marshal resource details to JSON: %w", err)
	}

	// Escape backslashes and double quotes for environment variable
	jsonString := string(resourcesJson)
	escapedJsonString := strings.ReplaceAll(jsonString, "\\", "\\\\")
	escapedJsonString = strings.ReplaceAll(escapedJsonString, "\"", "\\\"")

	return setEnvVar(ctx, azdClient, envName, "AI_PROJECT_DEPENDENT_RESOURCES", escapedJsonString)
}

func applicationEnvUpdate(ctx context.Context, appConfig *project.ApplicationSettings, azdClient *azdext.AzdClient, envName string) error {
	if err := setEnvVar(ctx, azdClient, envName, "ENABLE_APPLICATION", "true"); err != nil {
		return err
	}

	if appConfig.Name != "" {
		if err := setEnvVar(ctx, azdClient, envName, "APPLICATION_NAME", appConfig.Name); err != nil {
			return err
		}
	}

	if appConfig.BotService {
		if err := setEnvVar(ctx, azdClient, envName, "ENABLE_BOT_SERVICE", "true"); err != nil {
			return err
		}
	}

	return nil
}

func containerAgentHandling(ctx context.Context, azdClient *azdext.AzdClient, project *azdext.ProjectConfig, svc *azdext.ServiceConfig) error {
	servicePath := svc.RelativePath
	fullPath := filepath.Join(project.Path, servicePath)
	agentYamlPath := filepath.Join(fullPath, "agent.yaml")

	data, err := os.ReadFile(agentYamlPath)
	if err != nil {
		return nil
	}

	var agentDef agent_yaml.AgentDefinition
	if err := yaml.Unmarshal(data, &agentDef); err != nil {
		return fmt.Errorf("YAML content is not valid: %w", err)
	}

	// If there is an agent.yaml in the project, and it can be properly parsed into an agent definition, add the env var to enable container agents
	currentEnvResponse, err := azdClient.Environment().GetCurrent(ctx, &azdext.EmptyRequest{})
	if err != nil {
		return err
	}

	if err := setEnvVar(ctx, azdClient, currentEnvResponse.Environment.Name, "ENABLE_CONTAINER_AGENTS", "true"); err != nil {
		return err
	}

	return nil
}

func setEnvVar(ctx context.Context, azdClient *azdext.AzdClient, envName string, key string, value string) error {
	_, err := azdClient.Environment().SetValue(ctx, &azdext.SetEnvRequest{
		EnvName: envName,
		Key:     key,
		Value:   value,
	})
	if err != nil {
		return fmt.Errorf("failed to set environment variable %s=%s: %w", key, value, err)
	}

	fmt.Printf("Set environment variable: %s=%s\n", key, value)
	return nil
}

func setEnvVarIfEmpty(ctx context.Context, azdClient *azdext.AzdClient, envName string, key string, value string) error {
	resp, err := azdClient.Environment().GetValue(ctx, &azdext.GetEnvRequest{
		EnvName: envName,
		Key:     key,
	})
	if err == nil && resp.Value != "" {
		return nil
	}
	return setEnvVar(ctx, azdClient, envName, key, value)
}

func populateContainerSettings(ctx context.Context, azdClient *azdext.AzdClient, svc *azdext.ServiceConfig) error {
	var foundryAgentConfig *project.ServiceTargetAgentConfig
	if err := project.UnmarshalStruct(svc.Config, &foundryAgentConfig); err != nil {
		return fmt.Errorf("failed to parse foundry agent config: %w", err)
	}

	// Initialize result with existing values
	result := &project.ContainerSettings{}

	// Check and populate base object
	containerSettings := foundryAgentConfig.Container
	if containerSettings == nil {
		containerSettings = &project.ContainerSettings{}
	}

	// Check and populate Resources
	if containerSettings.Resources == nil {
		result.Resources = &project.ResourceSettings{}
	} else {
		result.Resources = &project.ResourceSettings{
			Memory: containerSettings.Resources.Memory,
			Cpu:    containerSettings.Resources.Cpu,
		}
	}

	// Check and populate Scale
	if containerSettings.Scale == nil {
		result.Scale = &project.ScaleSettings{}
	} else {
		result.Scale = &project.ScaleSettings{
			MinReplicas: containerSettings.Scale.MinReplicas,
			MaxReplicas: containerSettings.Scale.MaxReplicas,
		}
	}

	// Set default values if zero or empty
	if result.Resources.Memory == "" {
		result.Resources.Memory = project.DefaultMemory
	}

	if result.Resources.Cpu == "" {
		result.Resources.Cpu = project.DefaultCpu
	}

	if result.Scale.MinReplicas == 0 {
		result.Scale.MinReplicas = project.DefaultMinReplicas
	}

	if result.Scale.MaxReplicas == 0 {
		result.Scale.MaxReplicas = project.DefaultMaxReplicas
	}

	// Update the container settings in the existing config
	foundryAgentConfig.Container = result

	// Marshal the complete updated agent config back to the service config
	var agentConfigStruct *structpb.Struct
	var err error
	if agentConfigStruct, err = project.MarshalStruct(foundryAgentConfig); err != nil {
		return fmt.Errorf("failed to marshal agent config: %w", err)
	}

	svc.Config = agentConfigStruct

	// Need to add the service config back to the project for use further down the pipeline
	req := &azdext.AddServiceRequest{Service: svc}

	if _, err := azdClient.Project().AddService(ctx, req); err != nil {
		return fmt.Errorf("adding agent service to project: %w", err)
	}

	return nil
}

// applicationPostDeployHandler handles post-deploy for azure.ai.agent services that have
// Application resources enabled. It creates agent deployments and optionally publishes
// as a digital worker when Bot Service is also enabled.
func applicationPostDeployHandler(ctx context.Context, azdClient *azdext.AzdClient, args *azdext.ProjectEventArgs) error {
	azdEnvClient := azdClient.Environment()
	cEnvResponse, err := azdEnvClient.GetCurrent(ctx, &azdext.EmptyRequest{})
	if err != nil {
		return fmt.Errorf("failed to get current environment: %w", err)
	}

	envResponse, err := azdEnvClient.GetValues(ctx, &azdext.GetEnvironmentRequest{
		Name: cEnvResponse.Environment.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to get environment values: %w", err)
	}

	azdEnv := make(map[string]string, len(envResponse.KeyValues))
	for _, kv := range envResponse.KeyValues {
		azdEnv[kv.Key] = kv.Value
	}

	// Tier 1 guard: Only run when APPLICATION_NAME is set (Application was provisioned)
	applicationName := azdEnv["APPLICATION_NAME"]
	if applicationName == "" {
		return nil
	}

	projectID := azdEnv["AZURE_AI_PROJECT_ID"]
	if projectID == "" {
		return nil
	}

	// Parse project resource ID to extract components
	parsedResource, err := arm.ParseResourceID(projectID)
	if err != nil {
		return fmt.Errorf("failed to parse AZURE_AI_PROJECT_ID: %w", err)
	}

	subscriptionID := parsedResource.SubscriptionID
	resourceGroup := parsedResource.ResourceGroupName
	projectName := parsedResource.Name
	accountName := ""
	if parsedResource.Parent != nil {
		accountName = parsedResource.Parent.Name
	}
	if accountName == "" {
		return fmt.Errorf("could not extract account name from AZURE_AI_PROJECT_ID")
	}

	// Find agent name and version from deployed services
	var agentName, agentVersion string
	for _, svc := range args.Project.Services {
		if svc.Host == AiAgentHost {
			serviceKey := strings.ReplaceAll(svc.Name, " ", "_")
			serviceKey = strings.ReplaceAll(serviceKey, "-", "_")
			serviceKey = strings.ToUpper(serviceKey)

			agentName = azdEnv[fmt.Sprintf("AGENT_%s_NAME", serviceKey)]
			agentVersion = azdEnv[fmt.Sprintf("AGENT_%s_VERSION", serviceKey)]
			if agentName != "" && agentVersion != "" {
				break
			}
		}
	}

	if agentName == "" || agentVersion == "" {
		fmt.Println("No agent name/version found — skipping application deployment")
		return nil
	}

	// Get the tenant ID for credential
	tenantResponse, err := azdClient.Account().LookupTenant(ctx, &azdext.LookupTenantRequest{
		SubscriptionId: subscriptionID,
	})
	if err != nil {
		return fmt.Errorf("failed to get tenant ID: %w", err)
	}

	cred, err := azidentity.NewAzureDeveloperCLICredential(&azidentity.AzureDeveloperCLICredentialOptions{
		TenantID:                   tenantResponse.TenantId,
		AdditionallyAllowedTenants: []string{"*"},
	})
	if err != nil {
		return fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Tier 1: Create agent deployment on the Application
	fmt.Printf("Creating agent deployment on application '%s' (agent: %s, version: %s)...\n", applicationName, agentName, agentVersion)
	appClient := azure.NewApplicationClient(cred)
	if err := appClient.CreateAgentDeployment(ctx, subscriptionID, resourceGroup, accountName, projectName, applicationName, agentName, agentVersion); err != nil {
		return fmt.Errorf("failed to create agent deployment: %w", err)
	}
	fmt.Println("✓ Agent deployment created successfully")

	// Tier 2 guard: Only run A365 flow when Bot Service is enabled
	enableBotService := azdEnv["ENABLE_BOT_SERVICE"]
	blueprintID := azdEnv["AGENT_IDENTITY_BLUEPRINT_ID"]
	if enableBotService != "true" || blueprintID == "" {
		return nil
	}

	fmt.Println("Bot Service enabled — running A365 digital worker flow...")

	// Create OAuth2 permission grants for blueprint SP
	fmt.Println("Creating OAuth2 permission grants for blueprint service principal...")
	grantsClient, err := azure.NewOAuth2GrantsClient(cred)
	if err != nil {
		return fmt.Errorf("failed to create OAuth2 grants client: %w", err)
	}

	if err := grantsClient.CreateBlueprintOAuth2Grants(ctx, blueprintID); err != nil {
		fmt.Printf("Warning: OAuth2 grants creation failed: %v\n", err)
	} else {
		fmt.Println("✓ OAuth2 permission grants created")
	}

	// Publish digital worker to M365
	location := azdEnv["AZURE_LOCATION"]
	if location == "" {
		location = azdEnv["LOCATION"]
	}
	if location == "" {
		fmt.Println("Warning: AZURE_LOCATION not set — skipping M365 digital worker publish")
		return nil
	}

	fmt.Println("Publishing digital worker to Microsoft 365...")
	if err := appClient.PublishDigitalWorker(ctx, location, subscriptionID, resourceGroup, accountName, projectName, applicationName, blueprintID); err != nil {
		fmt.Printf("Warning: Digital worker publish failed: %v\n", err)
	} else {
		fmt.Println("✓ Digital worker published to Microsoft 365")
	}

	return nil
}
