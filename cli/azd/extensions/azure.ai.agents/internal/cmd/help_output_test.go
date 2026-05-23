// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentVariablesSection_HasExpectedKeys(t *testing.T) {
	got := environmentVariablesSection()
	// These names are the wire contract -- if any rename, doctor and the
	// project resolver also need updating, so a test pin prevents drift.
	for _, want := range []string{
		"Environments & Environment Variables:",
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
			"Environments & Environment Variables section missing %q", want)
	}
}

func TestDocsAndAgentSkillsSection_ListsAgentReadCommands(t *testing.T) {
	got := docsAndAgentSkillsSection()
	for _, want := range []string{
		"Docs & Agent Skills:",
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

// TestInstallAgentsHelpOutput_PreambleSeparatedByOneBlankLine pins the spacing
// between the "Get started" preamble and the cobra help body. The bug being
// guarded against: an extra `\n` (Fprintln vs Fprint) produced two blank lines
// between the preamble and the body, which was visually inconsistent with the
// single blank line separating the env-vars and docs sections below.
func TestInstallAgentsHelpOutput_PreambleSeparatedByOneBlankLine(t *testing.T) {
	// t.Chdir to a fresh temp dir so findAzureYaml returns false and we hit
	// the deterministic "No azd project detected" preamble branch -- no need
	// to spin up an azd client.
	t.Chdir(t.TempDir())

	rootCmd := &cobra.Command{
		Use:   "agent",
		Short: "COBRABODY-MARKER",
	}
	installAgentsHelpOutput(rootCmd)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	require.NoError(t, rootCmd.Help())

	output := buf.String()

	// Last visible text emitted by the preamble.
	const preambleTail = "agent project."
	tailIdx := strings.Index(output, preambleTail)
	require.GreaterOrEqual(t, tailIdx, 0, "preamble tail %q not found in output:\n%s", preambleTail, output)
	tailIdx += len(preambleTail)

	const bodyMarker = "COBRABODY-MARKER"
	bodyOffset := strings.Index(output[tailIdx:], bodyMarker)
	require.GreaterOrEqual(t, bodyOffset, 0, "body marker %q not found after preamble", bodyMarker)

	gap := output[tailIdx : tailIdx+bodyOffset]
	// Gap should be only whitespace -- nothing else lives between the
	// preamble's last line and the cobra body's Short text.
	assert.Equal(t, strings.TrimSpace(gap), "", "unexpected non-whitespace between preamble and body: %q", gap)

	// One blank line = exactly 2 newlines (one to terminate the preamble's
	// last line, one for the blank). Three or more newlines = the regression.
	newlines := strings.Count(gap, "\n")
	assert.Equal(t, 2, newlines,
		"expected 1 blank line (2 newlines) between preamble and cobra body, got %d newlines in %q",
		newlines, gap)
}
