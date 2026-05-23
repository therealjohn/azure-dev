// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCommand_HasAgentSubcommand(t *testing.T) {
	cmd := NewRootCommand()
	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "agent",
		"azd ai doc must expose the agent subgroup")
}

func TestNewRootCommand_RunsIndexAsDefault(t *testing.T) {
	cmd := NewRootCommand()
	// The bare `azd ai doc` invocation should render the index.
	// We assert by checking the RunE is wired -- the index is the only
	// place we set RunE on the root.
	require.NotNil(t, cmd.RunE,
		"azd ai doc with no subcommand should print the index via RunE")
}

func TestAgentSiblings_HasAgentEntry(t *testing.T) {
	// Locks in the wire contract: as new ai.* extensions adopt the docs
	// integration pattern, add them to this slice. The 'agent' entry must
	// stay first because doc_agent.go hard-references index 0.
	require.NotEmpty(t, agentSiblings,
		"agentSiblings must contain at least the agents entry")
	assert.Equal(t, "agent", agentSiblings[0].SubcommandName,
		"agent entry must be at index 0 (doc_agent.go relies on this)")
	assert.Equal(t, []string{"ai", "agent", "docs"}, agentSiblings[0].CommandPath,
		"agents extension exposes its docs via 'azd ai agent docs'")
}

func TestExtensionIDForSibling_KnownAgents(t *testing.T) {
	got := extensionIDForSibling(docSibling{SubcommandName: "agent"})
	assert.Equal(t, "azure.ai.agents", got,
		"agent subcommand maps to the azure.ai.agents extension ID")
}

func TestExtensionIDForSibling_UnknownFallsBackToName(t *testing.T) {
	got := extensionIDForSibling(docSibling{SubcommandName: "future-ext"})
	assert.Equal(t, "future-ext", got,
		"unknown sibling name passes through unchanged")
}
