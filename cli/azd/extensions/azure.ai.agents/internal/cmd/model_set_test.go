// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"azureaiagent/internal/exterrors"
	"azureaiagent/internal/pkg/agents/agent_yaml"
	"azureaiagent/internal/project"
	"errors"
	"testing"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/stretchr/testify/assert"
)

func TestModelSet_ValidateFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flags       modelSetFlags
		wantErr     bool
		wantErrCode string
	}{
		{
			name:    "no flags is valid",
			flags:   modelSetFlags{},
			wantErr: false,
		},
		{
			name:    "explicit positive capacity is valid",
			flags:   modelSetFlags{capacity: 50},
			wantErr: false,
		},
		{
			name:    "explicit non-empty version is valid",
			flags:   modelSetFlags{version: "2024-07-18", versionSet: true},
			wantErr: false,
		},
		{
			name:        "explicit empty version errors",
			flags:       modelSetFlags{version: "", versionSet: true},
			wantErr:     true,
			wantErrCode: exterrors.CodeInvalidParameter,
		},
		{
			name:        "explicit whitespace version errors",
			flags:       modelSetFlags{version: "   ", versionSet: true},
			wantErr:     true,
			wantErrCode: exterrors.CodeInvalidParameter,
		},
		{
			name:    "empty version without versionSet is valid (catalog fallback)",
			flags:   modelSetFlags{version: ""},
			wantErr: false,
		},
		{
			name:        "negative capacity errors",
			flags:       modelSetFlags{capacity: -1},
			wantErr:     true,
			wantErrCode: exterrors.CodeInvalidParameter,
		},
		{
			name:    "zero capacity is valid (uses default)",
			flags:   modelSetFlags{capacity: 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ModelSetAction{flags: &tt.flags}
			err := a.validateFlags()

			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}

			assert.Error(t, err)
			var extErr *azdext.LocalError
			if errors.As(err, &extErr) {
				assert.Equal(t, tt.wantErrCode, extErr.Code)
			} else {
				t.Fatalf("expected LocalError, got %T: %v", err, err)
			}
		})
	}
}

func TestModelSet_ApplyFlagOverrides(t *testing.T) {
	t.Parallel()

	baseResource := agent_yaml.ModelResource{
		Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
		Id:       "gpt-4o-mini",
		Version:  "2024-07-18",
		Sku:      "GlobalStandard",
		Capacity: 10,
		Format:   "OpenAI",
	}

	tests := []struct {
		name  string
		flags modelSetFlags
		want  agent_yaml.ModelResource
	}{
		{
			name:  "no flags preserves resource",
			flags: modelSetFlags{},
			want:  baseResource,
		},
		{
			name:  "version flag overrides when set",
			flags: modelSetFlags{version: "2024-08-06", versionSet: true},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-08-06",
				Sku:      "GlobalStandard",
				Capacity: 10,
				Format:   "OpenAI",
			},
		},
		{
			name:  "version not changed when versionSet false",
			flags: modelSetFlags{version: "ignored"},
			want:  baseResource,
		},
		{
			name:  "version trimmed before applying",
			flags: modelSetFlags{version: "  2024-08-06  ", versionSet: true},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-08-06",
				Sku:      "GlobalStandard",
				Capacity: 10,
				Format:   "OpenAI",
			},
		},
		{
			name:  "sku overrides when non-empty",
			flags: modelSetFlags{sku: "DataZoneStandard"},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-07-18",
				Sku:      "DataZoneStandard",
				Capacity: 10,
				Format:   "OpenAI",
			},
		},
		{
			name:  "positive capacity overrides",
			flags: modelSetFlags{capacity: 50},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-07-18",
				Sku:      "GlobalStandard",
				Capacity: 50,
				Format:   "OpenAI",
			},
		},
		{
			name:  "format overrides when non-empty",
			flags: modelSetFlags{format: "AzureOpenAI"},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-07-18",
				Sku:      "GlobalStandard",
				Capacity: 10,
				Format:   "AzureOpenAI",
			},
		},
		{
			name: "all flags apply together; Name/Kind preserved",
			flags: modelSetFlags{
				version: "2024-08-06", versionSet: true,
				sku: "DataZoneStandard", capacity: 25, format: "AzureOpenAI",
			},
			want: agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "gpt", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o-mini",
				Version:  "2024-08-06",
				Sku:      "DataZoneStandard",
				Capacity: 25,
				Format:   "AzureOpenAI",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ModelSetAction{flags: &tt.flags}
			got := a.applyFlagOverrides(baseResource)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindModelResource(t *testing.T) {
	t.Parallel()

	manifest := &agent_yaml.AgentManifest{
		Resources: []any{
			agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "primary", Kind: agent_yaml.ResourceKindModel},
				Id:       "gpt-4o",
			},
			agent_yaml.ModelResource{
				Resource: agent_yaml.Resource{Name: "embed", Kind: agent_yaml.ResourceKindModel},
				Id:       "text-embedding-3-small",
			},
		},
	}

	t.Run("finds existing model by id", func(t *testing.T) {
		idx, res, err := findModelResource(manifest, "text-embedding-3-small", "my-agent")
		assert.NoError(t, err)
		assert.Equal(t, 1, idx)
		assert.Equal(t, "embed", res.Name)
		assert.Equal(t, "text-embedding-3-small", res.Id)
	})

	t.Run("returns validation error when id not found", func(t *testing.T) {
		idx, _, err := findModelResource(manifest, "claude-sonnet", "my-agent")
		assert.Equal(t, -1, idx)
		assert.Error(t, err)
		var extErr *azdext.LocalError
		if errors.As(err, &extErr) {
			assert.Equal(t, exterrors.CodeModelResourceNotFound, extErr.Code)
		} else {
			t.Fatalf("expected LocalError, got %T: %v", err, err)
		}
	})

	t.Run("returns validation error for empty manifest", func(t *testing.T) {
		empty := &agent_yaml.AgentManifest{Resources: []any{}}
		idx, _, err := findModelResource(empty, "gpt-4o", "my-agent")
		assert.Equal(t, -1, idx)
		assert.Error(t, err)
	})
}

func TestDeploymentFromResource(t *testing.T) {
	t.Parallel()

	resource := agent_yaml.ModelResource{
		Resource: agent_yaml.Resource{Name: "primary", Kind: agent_yaml.ResourceKindModel},
		Id:       "gpt-4o-mini",
		Version:  "2024-07-18",
		Sku:      "GlobalStandard",
		Capacity: 10,
		Format:   "OpenAI",
	}

	want := project.Deployment{
		Name: "gpt-4o-mini",
		Model: project.DeploymentModel{
			Name:    "gpt-4o-mini",
			Format:  "OpenAI",
			Version: "2024-07-18",
		},
		Sku: project.DeploymentSku{
			Name:     "GlobalStandard",
			Capacity: 10,
		},
	}

	got := deploymentFromResource(resource)
	assert.Equal(t, want, got)
}

func TestIndexOfDeployment(t *testing.T) {
	t.Parallel()

	deployments := []project.Deployment{
		{Name: "gpt-4o"},
		{Name: "gpt-4o-mini"},
		{Name: "text-embedding-3-small"},
	}

	tests := []struct {
		name string
		want int
	}{
		{name: "gpt-4o", want: 0},
		{name: "gpt-4o-mini", want: 1},
		{name: "text-embedding-3-small", want: 2},
		{name: "claude-sonnet", want: -1},
		{name: "", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indexOfDeployment(deployments, tt.name)
			assert.Equal(t, tt.want, got)
		})
	}
}
