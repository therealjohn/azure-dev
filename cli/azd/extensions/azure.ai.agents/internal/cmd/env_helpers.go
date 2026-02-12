// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

// env_helpers.go provides shared helpers for standalone CLI commands (show, monitor)
// that need to read project configuration and Azure environment state.
//
// Why this file exists:
// The `listen` command and event handlers run inside the azd process and communicate
// via a gRPC client (azdext.AzdClient). However, standalone commands like `show` and
// `monitor` are invoked directly by the user (e.g. `azd ai agent show`) and do NOT
// have access to the azd gRPC client. They must read the project configuration
// (azure.yaml) and environment state (.azure/<env>/.env) directly from the filesystem
// to obtain agent names, versions, endpoints, and credentials.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// agentContext holds all the resolved information needed by standalone commands
// to interact with a deployed agent. It is populated by reading azure.yaml and
// the azd environment files directly from disk.
type agentContext struct {
	// ServiceName is the service key from azure.yaml (e.g. "seattle-hotel-agent")
	ServiceName string
	// AgentName is the deployed agent name (from AGENT_<KEY>_NAME env var)
	AgentName string
	// AgentVersion is the deployed agent version (from AGENT_<KEY>_VERSION env var)
	AgentVersion string
	// ProjectEndpoint is the Foundry project data-plane endpoint
	// (e.g. "https://ai-account-xxx.services.ai.azure.com/api/projects/ai-project-xxx")
	ProjectEndpoint string
	// ProjectResourceID is the ARM resource ID of the Foundry project
	// (e.g. "/subscriptions/.../providers/Microsoft.CognitiveServices/accounts/.../projects/...")
	ProjectResourceID string
	// AccountName is the Cognitive Services account name
	AccountName string
	// ProjectName is the Foundry project name
	ProjectName string
	// Credential is the Azure token credential for API calls
	Credential azcore.TokenCredential
	// Env holds all environment variable key-value pairs from .azure/<env>/.env
	Env map[string]string
}

// azdProjectConfig is a minimal representation of azure.yaml, containing only
// the fields we need to discover agent services.
type azdProjectConfig struct {
	Name     string                       `yaml:"name"`
	Services map[string]azdServiceConfig  `yaml:"services"`
}

// azdServiceConfig is a minimal representation of a service entry in azure.yaml.
type azdServiceConfig struct {
	Host string `yaml:"host"`
}

// azdEnvConfig represents .azure/config.json which stores the default environment name.
type azdEnvConfig struct {
	Version            int    `json:"version"`
	DefaultEnvironment string `json:"defaultEnvironment"`
}

// loadAgentContext reads the azure.yaml and azd environment files to build an
// agentContext for the specified service. If serviceName is empty, it defaults
// to the first service with host "azure.ai.agent".
//
// This function exists because standalone CLI commands cannot use the azd gRPC
// client — they run outside the azd process and must read configuration directly.
func loadAgentContext(serviceName string) (*agentContext, error) {
	// Step 1: Find the project root by locating azure.yaml
	projectDir, err := findProjectRoot()
	if err != nil {
		return nil, err
	}

	// Step 2: Parse azure.yaml to find agent services
	azureYamlPath := filepath.Join(projectDir, "azure.yaml")
	data, err := os.ReadFile(azureYamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read azure.yaml: %w", err)
	}

	var projConfig azdProjectConfig
	if err := yaml.Unmarshal(data, &projConfig); err != nil {
		return nil, fmt.Errorf("failed to parse azure.yaml: %w", err)
	}

	// Step 3: Resolve which service to use
	resolvedService, err := resolveServiceName(projConfig, serviceName)
	if err != nil {
		return nil, err
	}

	// Step 4: Determine the active azd environment name
	envName, err := resolveEnvironmentName(projectDir)
	if err != nil {
		return nil, err
	}

	// Step 5: Load environment variables from .azure/<env>/.env
	envFilePath := filepath.Join(projectDir, ".azure", envName, ".env")
	env, err := godotenv.Read(envFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read environment file %s: %w", envFilePath, err)
	}

	// Step 6: Extract agent-specific values using the service key convention
	// azd stores agent info as AGENT_<SERVICE_KEY>_NAME, AGENT_<SERVICE_KEY>_VERSION, etc.
	// where SERVICE_KEY is the service name uppercased with hyphens/spaces replaced by underscores.
	serviceKey := toServiceKey(resolvedService)
	agentName := env[fmt.Sprintf("AGENT_%s_NAME", serviceKey)]
	agentVersion := env[fmt.Sprintf("AGENT_%s_VERSION", serviceKey)]
	projectEndpoint := env["AZURE_AI_PROJECT_ENDPOINT"]
	projectResourceID := env["AZURE_AI_PROJECT_ID"]

	if agentName == "" || agentVersion == "" {
		return nil, fmt.Errorf(
			"agent not yet deployed for service %q. Run 'azd deploy' first "+
				"(missing AGENT_%s_NAME or AGENT_%s_VERSION in environment)",
			resolvedService, serviceKey, serviceKey,
		)
	}

	if projectEndpoint == "" {
		return nil, fmt.Errorf(
			"AZURE_AI_PROJECT_ENDPOINT not set. Run 'azd provision' or 'azd ai agent init --project-id <id>' first",
		)
	}

	// Step 7: Create Azure credential for API authentication
	// We use the same credential type as the rest of the extension (AzureDeveloperCLICredential)
	// which authenticates using the azd login session.
	tenantID := env["AZURE_TENANT_ID"]
	cred, err := azidentity.NewAzureDeveloperCLICredential(&azidentity.AzureDeveloperCLICredentialOptions{
		TenantID:                   tenantID,
		AdditionallyAllowedTenants: []string{"*"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Step 8: Extract account and project names from the resource ID (if available)
	var accountName, projectName string
	if projectResourceID != "" {
		parsedID, err := arm.ParseResourceID(projectResourceID)
		if err == nil {
			projectName = parsedID.Name
			if parsedID.Parent != nil {
				accountName = parsedID.Parent.Name
			}
		}
	}

	return &agentContext{
		ServiceName:       resolvedService,
		AgentName:         agentName,
		AgentVersion:      agentVersion,
		ProjectEndpoint:   projectEndpoint,
		ProjectResourceID: projectResourceID,
		AccountName:       accountName,
		ProjectName:       projectName,
		Credential:        cred,
		Env:               env,
	}, nil
}

// findProjectRoot walks up from the current directory to find azure.yaml,
// which marks the azd project root.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "azure.yaml")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding azure.yaml
			return "", fmt.Errorf("azure.yaml not found. Are you in an azd project directory?")
		}
		dir = parent
	}
}

// resolveServiceName selects which agent service to operate on.
// If serviceName is provided, it validates that it exists and is an agent host.
// If empty, it picks the first service with host "azure.ai.agent".
func resolveServiceName(config azdProjectConfig, serviceName string) (string, error) {
	if serviceName != "" {
		svc, ok := config.Services[serviceName]
		if !ok {
			return "", fmt.Errorf("service %q not found in azure.yaml", serviceName)
		}
		if svc.Host != AiAgentHost {
			return "", fmt.Errorf("service %q has host %q, expected %q", serviceName, svc.Host, AiAgentHost)
		}
		return serviceName, nil
	}

	// Auto-select the first azure.ai.agent service
	for name, svc := range config.Services {
		if svc.Host == AiAgentHost {
			return name, nil
		}
	}

	return "", fmt.Errorf("no services with host %q found in azure.yaml", AiAgentHost)
}

// resolveEnvironmentName determines which azd environment is active by reading
// .azure/config.json for the defaultEnvironment setting.
func resolveEnvironmentName(projectDir string) (string, error) {
	configPath := filepath.Join(projectDir, ".azure", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read .azure/config.json: %w", err)
	}

	var config azdEnvConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("failed to parse .azure/config.json: %w", err)
	}

	if config.DefaultEnvironment == "" {
		return "", fmt.Errorf("no default environment set in .azure/config.json. Run 'azd env select' first")
	}

	return config.DefaultEnvironment, nil
}

// toServiceKey converts a service name to the environment variable key format
// used by azd: uppercase, hyphens and spaces replaced with underscores.
// For example, "seattle-hotel-agent" becomes "SEATTLE_HOTEL_AGENT".
func toServiceKey(serviceName string) string {
	key := strings.ReplaceAll(serviceName, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	return strings.ToUpper(key)
}

// buildPortalURL constructs a Foundry Portal URL for a given page (e.g. "build", "monitor").
// The portal URL format encodes the subscription ID as base64 and includes the resource group,
// account name, and project name as comma-separated path segments.
func buildPortalURL(projectResourceID, agentName, agentVersion, page string) (string, error) {
	resourceID, err := arm.ParseResourceID(projectResourceID)
	if err != nil {
		return "", fmt.Errorf("failed to parse project resource ID: %w", err)
	}

	// The Foundry portal encodes the subscription ID as URL-safe base64 without padding
	encodedSubID, err := encodeSubscriptionIDForPortal(resourceID.SubscriptionID)
	if err != nil {
		return "", err
	}

	resourceGroup := resourceID.ResourceGroupName
	if resourceID.Parent == nil {
		return "", fmt.Errorf("invalid Foundry project resource ID: missing parent account")
	}
	accountName := resourceID.Parent.Name
	projectName := resourceID.Name

	return fmt.Sprintf(
		"https://ai.azure.com/nextgen/r/%s,%s,,%s,%s/build/agents/%s/%s?version=%s",
		encodedSubID, resourceGroup, accountName, projectName, agentName, page, agentVersion,
	), nil
}

// encodeSubscriptionIDForPortal encodes a subscription GUID as URL-safe base64
// without padding, matching the Foundry portal URL format.
// This is the same encoding used by the existing agentPlaygroundUrl function
// in service_target_agent.go (encodeSubscriptionID).
func encodeSubscriptionIDForPortal(subscriptionID string) (string, error) {
	guid, err := uuid.Parse(subscriptionID)
	if err != nil {
		return "", fmt.Errorf("invalid subscription ID format: %w", err)
	}

	guidBytes, _ := guid.MarshalBinary()
	encoded := base64.URLEncoding.EncodeToString(guidBytes)
	return strings.TrimRight(encoded, "="), nil
}
