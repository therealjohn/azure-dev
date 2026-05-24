// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// help_styling_test.go covers end-to-end --help output for representative
// commands in the docs extension. See the agents-side mirror file for
// detailed rationale on color-toggling and the helpOf helper shape.

func withColorDisabled(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

func helpOf(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCommand()
	root.SilenceErrors = true
	root.SilenceUsage = true

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append(args, "--help"))
	require.NoError(t, root.Execute(), "Execute(%v --help) returned error", args)
	return buf.String()
}

// TestDocRootHelp_StyledSections asserts the migrated Examples block
// renders AND the styled section headers appear under the doc root.
// The previous inline "Examples:" prose inside Long must be gone --
// otherwise the help output would show TWO Examples blocks (the
// legacy inline one and the migrated styled one).
func TestDocRootHelp_StyledSections(t *testing.T) {
	withColorDisabled(t)

	out := helpOf(t)

	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Available Commands:")

	// Migrated Examples block (from cmd.Example -> auto-promoted).
	assert.Contains(t, out, "Examples:")
	assert.Contains(t, out, "List ai.* extensions with docs.",
		"first migrated example title missing")
	assert.Contains(t, out, "List topics for the agents extension.")
	assert.Contains(t, out, "Print one topic's markdown.")

	// Available Commands listing should include the 3 visible leaves
	// (agent, skills, version; metadata is reserved by the SDK and may
	// appear as well -- not asserted).
	for _, name := range []string{"agent", "skills", "version"} {
		assert.True(t, strings.Contains(out, name),
			"Available Commands missing %q", name)
	}
}

// TestDocRootHelp_NoLegacyExamplesInLong is the regression for the
// "remove inline Examples from Long when migrating" rule. The Long
// field of the root must NOT still contain "Examples:" prose -- if
// it does, future template changes could double-render an unstyled
// Examples section alongside the styled one.
func TestDocRootHelp_NoLegacyExamplesInLong(t *testing.T) {
	root := NewRootCommand()
	assert.NotContains(t, root.Long, "Examples:",
		"root.Long must not contain 'Examples:' prose after migration; cmd.Example holds the migrated source for helpformat.Install to auto-promote")
}

// TestDocAgentHelp_Smoke confirms the agent (topic) command gets
// styled sections and that its migrated Examples block appears.
func TestDocAgentHelp_Smoke(t *testing.T) {
	withColorDisabled(t)

	out := helpOf(t, "agent")
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Global Flags:")
	assert.Contains(t, out, "Examples:", "agent has examples migrated from cmd.Example")
	assert.Contains(t, out, "List topics.", "first migrated example title missing")
}

// TestDocSkillsInstallHelp_BulletPreambleAndExamples confirms the
// long-form skill install command -- which has an existing Long
// containing bullet items written into the cobra.Command literal --
// renders those as plain text alongside the styled section headers
// and migrated Examples. This is the "leave existing Long verbatim"
// path: no Description override, just styling around it.
func TestDocSkillsInstallHelp_BulletPreambleAndExamples(t *testing.T) {
	withColorDisabled(t)

	out := helpOf(t, "skills", "install")
	assert.Contains(t, out, "Built-in targets:")
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Flags:")
	assert.Contains(t, out, "--target", "install's --target flag should appear in Flags section")
	assert.Contains(t, out, "Examples:", "skills install has migrated examples")
}
