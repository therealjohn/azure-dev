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

	"azureaiagent/internal/version"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/azure/azure-dev/cli/azd/pkg/azsdk"
	"github.com/azure/azure-dev/cli/azd/pkg/graphsdk"
)

const (
	// Well-known app IDs
	apxAppID     = "5a807f24-c9de-44ee-a3a7-329e88a00ffc"
	prodMCPAppID = "ea9ffc3e-8a23-4a7d-836d-234d7c7565c1"

	// MCP server scopes
	mcpScopes = "McpServers.M365Admin.All McpServers.DASearch.All McpServers.WebSearch.All McpServers.Files.All " +
		"AgentTools.MOSEvents.All McpServers.Admin365Graph.All McpServers.ERPAnalytics.All McpServers.DataverseCustom.All " +
		"McpServers.Dataverse.All McpServers.D365Service.All McpServers.D365Sales.All McpServers.Management.All " +
		"McpServersMetadata.Read.All McpServers.Developer.All McpServers.CopilotMCP.All McpServers.OneDriveSharepoint.All " +
		"McpServers.Mail.All McpServers.Teams.All McpServers.Me.All McpServers.Calendar.All McpServers.SharepointLists.All " +
		"McpServers.Knowledge.All McpServers.Excel.All McpServers.Word.All McpServers.PowerPoint.All"

	// APX scopes
	apxScopes = "AgentData.ReadWrite"
)

// OAuth2GrantsClient provides methods for creating OAuth2 permission grants via Microsoft Graph
type OAuth2GrantsClient struct {
	pipeline    runtime.Pipeline
	graphClient *graphsdk.GraphClient
}

// NewOAuth2GrantsClient creates a new OAuth2GrantsClient
func NewOAuth2GrantsClient(cred azcore.TokenCredential) (*OAuth2GrantsClient, error) {
	userAgent := fmt.Sprintf("azd-ext-azure-ai-agents/%s", version.Version)

	graphOptions := &policy.ClientOptions{
		PerCallPolicies: []policy.Policy{
			runtime.NewBearerTokenPolicy(cred, []string{"https://graph.microsoft.com/.default"}, nil),
			azsdk.NewMsCorrelationPolicy(),
			azsdk.NewUserAgentPolicy(userAgent),
		},
	}

	pipeline := runtime.NewPipeline("azure-ai-agents", "v1.0.0", runtime.PipelineOptions{}, graphOptions)

	graphClient, err := graphsdk.NewGraphClient(cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph client: %w", err)
	}

	return &OAuth2GrantsClient{
		pipeline:    pipeline,
		graphClient: graphClient,
	}, nil
}

// oauth2PermissionGrant represents an OAuth2 permission grant request
type oauth2PermissionGrant struct {
	ClientID    string  `json:"clientId"`
	ConsentType string  `json:"consentType"`
	PrincipalID *string `json:"principalId"`
	ResourceID  string  `json:"resourceId"`
	Scope       string  `json:"scope"`
}

// CreateBlueprintOAuth2Grants creates the required OAuth2 permission grants for the blueprint service principal.
// This is idempotent — "already exists" errors are handled gracefully.
func (c *OAuth2GrantsClient) CreateBlueprintOAuth2Grants(ctx context.Context, blueprintID string) error {
	// Resolve the blueprint SP object ID from client/app ID
	blueprintSP, err := c.getServicePrincipalByAppID(ctx, blueprintID)
	if err != nil {
		return fmt.Errorf("failed to get blueprint service principal: %w", err)
	}

	// Resolve MCP and APX service principal object IDs
	mcpSP, err := c.getServicePrincipalByAppID(ctx, prodMCPAppID)
	if err != nil {
		return fmt.Errorf("failed to get MCP service principal: %w", err)
	}

	apxSP, err := c.getServicePrincipalByAppID(ctx, apxAppID)
	if err != nil {
		return fmt.Errorf("failed to get APX service principal: %w", err)
	}

	// Create MCP OAuth2 grant
	fmt.Println("Creating MCP OAuth2 permission grant...")
	if err := c.createGrant(ctx, blueprintSP, mcpSP, mcpScopes); err != nil {
		return fmt.Errorf("failed to create MCP OAuth2 grant: %w", err)
	}

	// Create APX OAuth2 grant
	fmt.Println("Creating APX OAuth2 permission grant...")
	if err := c.createGrant(ctx, blueprintSP, apxSP, apxScopes); err != nil {
		return fmt.Errorf("failed to create APX OAuth2 grant: %w", err)
	}

	return nil
}

func (c *OAuth2GrantsClient) getServicePrincipalByAppID(ctx context.Context, appID string) (string, error) {
	sp, err := c.graphClient.ServicePrincipals().
		Filter(fmt.Sprintf("appId eq '%s'", appID)).
		Get(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to query service principal for appId %s: %w", appID, err)
	}

	if len(sp.Value) == 0 {
		return "", fmt.Errorf("service principal not found for appId %s", appID)
	}

	if sp.Value[0].Id == nil {
		return "", fmt.Errorf("service principal ID is nil for appId %s", appID)
	}

	return *sp.Value[0].Id, nil
}

func (c *OAuth2GrantsClient) createGrant(ctx context.Context, clientID, resourceID, scope string) error {
	grant := oauth2PermissionGrant{
		ClientID:    clientID,
		ConsentType: "AllPrincipals",
		PrincipalID: nil,
		ResourceID:  resourceID,
		Scope:       scope,
	}

	payload, err := json.Marshal(grant)
	if err != nil {
		return fmt.Errorf("failed to marshal grant request: %w", err)
	}

	url := "https://graph.microsoft.com/v1.0/oauth2PermissionGrants"
	req, err := runtime.NewRequest(ctx, http.MethodPost, url)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := req.SetBody(streaming.NopCloser(bytes.NewReader(payload)), "application/json"); err != nil {
		return fmt.Errorf("failed to set request body: %w", err)
	}

	resp, err := c.pipeline.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if runtime.HasStatusCode(resp, http.StatusOK, http.StatusCreated) {
		fmt.Println("✓ OAuth2 permission grant created")
		return nil
	}

	// Check for "already exists" error
	respBody, _ := io.ReadAll(resp.Body)
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(respBody, &errResp) == nil &&
		errResp.Error.Code == "Request_BadRequest" &&
		containsSubstr(errResp.Error.Message, "Permission entry already exists") {
		fmt.Println("✓ Permission already exists — skipping")
		return nil
	}

	return fmt.Errorf("failed to create OAuth2 grant (status %d): %s", resp.StatusCode, string(respBody))
}
