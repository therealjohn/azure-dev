// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"azureaiagent/internal/version"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/azure/azure-dev/cli/azd/pkg/azsdk"
)

// ApplicationClient provides methods for managing Cognitive Services Application resources
type ApplicationClient struct {
	armPipeline runtime.Pipeline
	aiPipeline  runtime.Pipeline
}

// NewApplicationClient creates a new ApplicationClient with both ARM and AI data-plane pipelines
func NewApplicationClient(cred azcore.TokenCredential) *ApplicationClient {
	userAgent := fmt.Sprintf("azd-ext-azure-ai-agents/%s", version.Version)

	armOptions := &policy.ClientOptions{
		PerCallPolicies: []policy.Policy{
			runtime.NewBearerTokenPolicy(cred, []string{"https://management.azure.com/.default"}, nil),
			azsdk.NewMsCorrelationPolicy(),
			azsdk.NewUserAgentPolicy(userAgent),
		},
	}

	aiOptions := &policy.ClientOptions{
		PerCallPolicies: []policy.Policy{
			runtime.NewBearerTokenPolicy(cred, []string{"https://ai.azure.com/.default"}, nil),
			azsdk.NewMsCorrelationPolicy(),
			azsdk.NewUserAgentPolicy(userAgent),
		},
	}

	return &ApplicationClient{
		armPipeline: runtime.NewPipeline("azure-ai-agents", "v1.0.0", runtime.PipelineOptions{}, armOptions),
		aiPipeline:  runtime.NewPipeline("azure-ai-agents", "v1.0.0", runtime.PipelineOptions{}, aiOptions),
	}
}

// AgentDeploymentRequest represents the request body for creating an agent deployment
type AgentDeploymentRequest struct {
	Properties AgentDeploymentProperties `json:"properties"`
}

// AgentDeploymentProperties represents the properties of an agent deployment
type AgentDeploymentProperties struct {
	DisplayName    string                   `json:"displayName"`
	Protocols      []AgentDeploymentProtocol `json:"protocols"`
	Agents         []AgentDeploymentAgent    `json:"agents"`
	DeploymentType string                   `json:"deploymentType"`
	MinReplicas    int                      `json:"minReplicas"`
	MaxReplicas    int                      `json:"maxReplicas"`
}

// AgentDeploymentProtocol represents a protocol supported by the deployment
type AgentDeploymentProtocol struct {
	Protocol string `json:"protocol"`
	Version  string `json:"version"`
}

// AgentDeploymentAgent represents an agent reference in the deployment
type AgentDeploymentAgent struct {
	AgentName    string `json:"agentName"`
	AgentVersion string `json:"agentVersion"`
}

// AgentDeploymentResponse represents the response from the agent deployment API (polling)
type AgentDeploymentResponse struct {
	Properties struct {
		State             string `json:"state"`
		ProvisioningState string `json:"provisioningState"`
	} `json:"properties"`
}

// AgentDeploymentGetResponse represents the full response from GET agent deployment
type AgentDeploymentGetResponse struct {
	Properties struct {
		State             string                 `json:"state"`
		ProvisioningState string                 `json:"provisioningState"`
		Agents            []AgentDeploymentAgent `json:"agents"`
	} `json:"properties"`
}

// CreateAgentDeployment creates an agent deployment on a Cognitive Services Application.
// The ARM RP does not support PUT updates or DELETE on existing agentDeployments,
// so if one already exists it is left as-is.
func (c *ApplicationClient) CreateAgentDeployment(
	ctx context.Context,
	subscriptionID, resourceGroup, accountName, projectName, applicationName string,
	agentName, agentVersion string,
) error {
	deploymentName := "foundry-agent-deployment"
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.CognitiveServices/accounts/%s/projects/%s/applications/%s/agentDeployments/%s?api-version=2025-10-01-preview",
		subscriptionID, resourceGroup, accountName, projectName, applicationName, deploymentName,
	)

	// Check if deployment already exists — ARM RP doesn't support updates or deletes
	existing, err := c.getAgentDeployment(ctx, url)
	if err == nil && existing != nil {
		fmt.Printf("Agent deployment '%s' already exists (state: %s). Skipping.\n",
			deploymentName, existing.Properties.State)
		return nil
	}

	// Wait for the application's blueprint identity to finish provisioning
	if err := c.waitForBlueprintReady(ctx, subscriptionID, resourceGroup, accountName, projectName, applicationName); err != nil {
		return err
	}

	body := AgentDeploymentRequest{
		Properties: AgentDeploymentProperties{
			DisplayName: "Foundry Agent Deployment",
			Protocols: []AgentDeploymentProtocol{
				{Protocol: "Activity", Version: "v1"},
			},
			Agents: []AgentDeploymentAgent{
				{AgentName: agentName, AgentVersion: agentVersion},
			},
			DeploymentType: "Hosted",
			MinReplicas:    1,
			MaxReplicas:    1,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal agent deployment request: %w", err)
	}

	req, err := runtime.NewRequest(ctx, http.MethodPut, url)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := req.SetBody(streaming.NopCloser(bytes.NewReader(payload)), "application/json"); err != nil {
		return fmt.Errorf("failed to set request body: %w", err)
	}

	resp, err := c.armPipeline.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create agent deployment: %w", err)
	}
	defer resp.Body.Close()

	if !runtime.HasStatusCode(resp, http.StatusOK, http.StatusCreated, http.StatusAccepted) {
		return runtime.NewResponseError(resp)
	}

	// Poll for completion
	return c.pollAgentDeployment(ctx, url)
}

// ApplicationResponse represents the GET response for a Cognitive Services Application
type ApplicationResponse struct {
	Properties struct {
		AgentIdentityBlueprint struct {
			ProvisioningState string `json:"provisioningState"`
		} `json:"agentIdentityBlueprint"`
	} `json:"properties"`
}

// waitForBlueprintReady polls the application until the agentIdentityBlueprint is no longer "Creating".
func (c *ApplicationClient) waitForBlueprintReady(
	ctx context.Context,
	subscriptionID, resourceGroup, accountName, projectName, applicationName string,
) error {
	appURL := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.CognitiveServices/accounts/%s/projects/%s/applications/%s?api-version=2025-10-01-preview",
		subscriptionID, resourceGroup, accountName, projectName, applicationName,
	)

	for i := 0; i < 24; i++ { // up to ~2 minutes
		req, err := runtime.NewRequest(ctx, http.MethodGet, appURL)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.armPipeline.Do(req)
		if err != nil {
			return fmt.Errorf("failed to get application: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read application response: %w", err)
		}

		var app ApplicationResponse
		if err := json.Unmarshal(body, &app); err != nil {
			return fmt.Errorf("failed to parse application response: %w", err)
		}

		state := app.Properties.AgentIdentityBlueprint.ProvisioningState
		if state != "Creating" {
			return nil
		}

		fmt.Printf("Waiting for application blueprint to finish provisioning (state: %s)...\n", state)
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for application blueprint to finish provisioning")
}

// getAgentDeployment fetches an existing agent deployment, returning nil if not found.
func (c *ApplicationClient) getAgentDeployment(ctx context.Context, url string) (*AgentDeploymentGetResponse, error) {
	req, err := runtime.NewRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.armPipeline.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if !runtime.HasStatusCode(resp, http.StatusOK) {
		return nil, runtime.NewResponseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var deployment AgentDeploymentGetResponse
	if err := json.Unmarshal(body, &deployment); err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (c *ApplicationClient) pollAgentDeployment(ctx context.Context, url string) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for agent deployment to complete")
		case <-ticker.C:
			req, err := runtime.NewRequest(ctx, http.MethodGet, url)
			if err != nil {
				return fmt.Errorf("failed to create poll request: %w", err)
			}

			resp, err := c.armPipeline.Do(req)
			if err != nil {
				return fmt.Errorf("failed to poll agent deployment: %w", err)
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to read poll response: %w", err)
			}

			var deployment AgentDeploymentResponse
			if err := json.Unmarshal(body, &deployment); err != nil {
				return fmt.Errorf("failed to parse poll response: %w", err)
			}

			if deployment.Properties.State == "Running" {
				return nil
			}
			if deployment.Properties.ProvisioningState == "Failed" {
				return fmt.Errorf("agent deployment failed")
			}

			fmt.Printf("Agent deployment in progress (state: %s)...\n", deployment.Properties.ProvisioningState)
		}
	}
}

// Microsoft365PublishRequest represents the request body for publishing a digital worker
type Microsoft365PublishRequest struct {
	BotID                    string                    `json:"botId"`
	PublishAsDigitalWorker   bool                      `json:"publishAsDigitalWorker"`
	AppPublishScope          string                    `json:"appPublishScope"`
	SubscriptionID           string                    `json:"subscriptionId"`
	AgentName                string                    `json:"agentName"`
	AppVersion               string                    `json:"appVersion"`
	ShortDescription         string                    `json:"shortDescription"`
	FullDescription          string                    `json:"fullDescription"`
	DeveloperName            string                    `json:"developerName"`
	DeveloperWebsiteURL      string                    `json:"developerWebsiteUrl"`
	PrivacyURL               string                    `json:"privacyUrl"`
	TermsOfUseURL            string                    `json:"termsOfUseUrl"`
	UseAgenticUserTemplate   bool                      `json:"useAgenticUserTemplate"`
	AgenticUserTemplate      *AgenticUserTemplate      `json:"agenticUserTemplate,omitempty"`
}

// AgenticUserTemplate represents the agentic user template for M365 publishing
type AgenticUserTemplate struct {
	ID                       string `json:"Id"`
	File                     string `json:"File"`
	SchemaVersion            string `json:"SchemaVersion"`
	AgentIdentityBlueprintID string `json:"AgentIdentityBlueprintId"`
	CommunicationProtocol    string `json:"CommunicationProtocol"`
}

// PublishDigitalWorker publishes the agent as a digital worker to Microsoft 365.
func (c *ApplicationClient) PublishDigitalWorker(
	ctx context.Context,
	location, subscriptionID, resourceGroup, accountName, projectName string,
	applicationName, blueprintID string,
) error {
	workspaceName := fmt.Sprintf("%s@%s@AML", accountName, projectName)
	url := fmt.Sprintf(
		"https://%s.api.azureml.ms/agent-asset/v2.0/subscriptions/%s/resourceGroups/%s/providers/Microsoft.MachineLearningServices/workspaces/%s/microsoft365/publish",
		location, subscriptionID, resourceGroup, workspaceName,
	)

	body := Microsoft365PublishRequest{
		BotID:                  blueprintID,
		PublishAsDigitalWorker: true,
		AppPublishScope:        "Tenant",
		SubscriptionID:         subscriptionID,
		AgentName:              applicationName,
		AppVersion:             "1.0.0",
		ShortDescription:       "Foundry agent deployed via Azure Developer CLI",
		FullDescription:        "A Foundry agent integrated with Microsoft 365 via Azure Developer CLI.",
		DeveloperName:          "Azure Developer",
		DeveloperWebsiteURL:    "https://azure.microsoft.com",
		PrivacyURL:             "https://privacy.microsoft.com",
		TermsOfUseURL:          "https://www.microsoft.com/legal/terms-of-use",
		UseAgenticUserTemplate: true,
		AgenticUserTemplate: &AgenticUserTemplate{
			ID:                       "digitalWorkerTemplate",
			File:                     "agenticUserTemplateManifest.json",
			SchemaVersion:            "0.1.0-preview",
			AgentIdentityBlueprintID: blueprintID,
			CommunicationProtocol:    "activityProtocol",
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal publish request: %w", err)
	}

	req, err := runtime.NewRequest(ctx, http.MethodPost, url)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := req.SetBody(streaming.NopCloser(bytes.NewReader(payload)), "application/json"); err != nil {
		return fmt.Errorf("failed to set request body: %w", err)
	}

	resp, err := c.aiPipeline.Do(req)
	if err != nil {
		return fmt.Errorf("failed to publish digital worker: %w", err)
	}
	defer resp.Body.Close()

	if !runtime.HasStatusCode(resp, http.StatusOK, http.StatusCreated) {
		// Check for "version already exists" error and treat as success
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil &&
			errResp.Error.Code == "UserError" &&
			contains(errResp.Error.Message, "version already exists") {
			fmt.Println("Digital worker already published with this version. Skipping.")
			return nil
		}
		return fmt.Errorf("failed to publish digital worker (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
