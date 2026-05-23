// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentVariablesSection_HasExpectedKeys(t *testing.T) {
	got := environmentVariablesSection()
	// These names are the wire contract -- if any rename, doctor and the
	// project resolver also need updating, so a test pin prevents drift.
	for _, want := range []string{
		"ENVIRONMENT VARIABLES",
		"azd env list",
		"azd env new",
		"azd env select",
		"azd env get",
		"azd env set",
		"AZURE_AI_PROJECT_ENDPOINT",
		"FOUNDRY_PROJECT_ENDPOINT",
		"AZURE_AI_PROJECT_ID",
		"AGENT_<SVC>_<PROTO>_ENDPOINT",
		"AGENT_<SVC>_ENDPOINT",
		"AI_AGENT_PENDING_PROVISION",
	} {
		assert.True(t, strings.Contains(got, want),
			"ENVIRONMENT VARIABLES section missing %q", want)
	}
}

func TestDocsAndAgentSkillsSection_ListsAgentReadCommands(t *testing.T) {
	got := docsAndAgentSkillsSection()
	for _, want := range []string{
		"DOCS & AGENT SKILLS",
		"azd ai agent show --output json",
		"azd ai agent project show --output json",
		"azd ai agent doctor --output json",
		"azd ext install azure.ai.docs",
		"azd ai doc",
		"azd ai doc agent",
		"azd ai doc agent <topic>",
	} {
		assert.True(t, strings.Contains(got, want),
			"DOCS section missing %q", want)
	}
}

func TestFormatGetStarted_RendersHeaderAndLines(t *testing.T) {
	got := formatGetStarted("Header here:", "first  Description 1.", "second  Description 2.")
	assert.True(t, strings.Contains(got, "Header here:"), "header missing")
	assert.True(t, strings.Contains(got, "first  Description 1."), "first line missing")
	assert.True(t, strings.Contains(got, "second  Description 2."), "second line missing")
}

func TestFindAzureYaml_NotFound_ReturnsFalseInTempDir(t *testing.T) {
	// Run from a directory guaranteed to be outside any azd project: t.TempDir.
	// chdir-isolation is t.Chdir's whole job.
	t.Chdir(t.TempDir())
	_, found := findAzureYaml()
	assert.False(t, found, "findAzureYaml should return false in an empty temp dir")
}
