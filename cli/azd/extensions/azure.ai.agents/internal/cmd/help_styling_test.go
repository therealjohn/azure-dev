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
// commands in the agents extension. These tests are the safety net against
// regressions in either the helpformat helper or per-command wiring -- if
// either side breaks, these snapshots-by-substring fail.
//
// Color is disabled in each test (no t.Parallel) because helpformat builds
// its UsageTemplate at package-init time using whatever color.NoColor was
// set then; toggling color at runtime after that init point only affects
// the Examples/Description renderers, not the section-header escapes.
// Asserting on plain text avoids both code paths' coupling.

func withColorDisabled(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

// helpOf runs `args... --help` against a fresh root command and returns
// the captured stdout+stderr. cobra writes help to stderr by default
// in some configurations; capturing both lets the assertions ignore
// the source stream.
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

// TestAgentRootHelp_StyledSections asserts the root --help output keeps
// the existing custom layout (banner, state-aware preamble, env vars,
// docs sections) AND now includes styled middle sections delivered by
// the new UsageTemplate. Avoids asserting on exact whitespace so minor
// cobra-template tweaks don't break the test.
func TestAgentRootHelp_StyledSections(t *testing.T) {
	withColorDisabled(t)
	t.Chdir(t.TempDir())

	out := helpOf(t)

	// Custom HelpFunc-driven sections from help_output.go (preserved).
	assert.Contains(t, out, "Ship agents with Microsoft Foundry from your terminal.",
		"root Short description should appear at the top")
	assert.Contains(t, out, "Environments & Environment Variables:",
		"existing env vars section should still render")
	assert.Contains(t, out, "Docs & Agent Skills:",
		"existing docs section should still render")

	// Styled middle (from UsageTemplate).
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Available Commands:")
	assert.Contains(t, out, "Flags:")
	assert.Contains(t, out, "Global Flags:")
}

// TestAgentInitHelp_HasBulletsAndExamples confirms init's custom
// Description bullets render AND its migrated Examples block appears.
// This is the regression for the manual-override path in helpformat.
func TestAgentInitHelp_HasBulletsAndExamples(t *testing.T) {
	withColorDisabled(t)
	t.Chdir(t.TempDir())

	out := helpOf(t, "init")

	// Bullets from getCmdInitHelpDescription.
	assert.Contains(t, out, "  * Running azd ai agent init", "first bullet missing")
	assert.Contains(t, out, "  * Use --manifest to point at", "manifest/from-code bullet missing")
	assert.Contains(t, out, "  * In --no-prompt mode pass --from-code", "no-prompt bullet missing")

	// Styled Examples footer (migrated from cmd.Example into helpformat.Examples).
	assert.Contains(t, out, "Examples:")
	assert.Contains(t, out, "Initialize from an agent manifest.")
	assert.Contains(t, out, "Non-interactive code deploy (CI/CD or agent-driven flows).")
}

// TestAgentInitHelp_NoLegacyExampleField is the regression for the
// "don't double-render examples" rule from rubber-duck-impl review.
// After migration, init must not still have a cobra.Command.Example
// string lying around that could spawn an unstyled second Examples
// section in some future template.
func TestAgentInitHelp_NoLegacyExampleField(t *testing.T) {
	// No color toggle needed -- only inspects cobra.Command state.
	root := NewRootCommand()
	for _, c := range root.Commands() {
		if c.Name() == "init" {
			assert.Equal(t, "", c.Example,
				"init must clear cmd.Example after migration; otherwise the cobra default template could double-render examples")
			return
		}
	}
	t.Fatal("init subcommand not found on root")
}

// TestAgentMcpHelp_NoBullets confirms helpformat doesn't synthesize
// bullets where there are none. The mcp parent is hidden but its
// subcommands are not -- a future regression that promoted mcp from
// hidden would invalidate this test, in which case adjust to a
// different no-bullet command (monitor, show, ...).
func TestAgentMcpHelp_NoBullets(t *testing.T) {
	withColorDisabled(t)
	t.Chdir(t.TempDir())

	// `mcp` is Hidden, so it is reachable via --help but does not
	// appear in the parent's Available Commands listing.
	out := helpOf(t, "mcp")
	assert.NotContains(t, out, "  * ",
		"mcp has no Description bullets configured; the '  * ' prefix must not appear")
}

// TestAgentDoctorHelp_Smoke confirms a typical leaf command gets styled
// sections and an auto-migrated Examples block.
func TestAgentDoctorHelp_Smoke(t *testing.T) {
	withColorDisabled(t)
	t.Chdir(t.TempDir())

	out := helpOf(t, "doctor")
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "Flags:")
	assert.Contains(t, out, "Global Flags:")
	assert.Contains(t, out, "Examples:", "doctor has examples migrated from cmd.Example")
	assert.Contains(t, out, "--local-only", "doctor's local flag should appear")
}

// TestAgentConnectionHelp_StyledByInstallAll is the regression test for
// the bulk-wiring path. The connection root and its subcommands live in
// a separate Go package (internal/connections/cmd) and were styled by
// helpformat.InstallAll walking the tree -- not by an import in the
// connections package itself. Verifies the recursion reaches across
// package boundaries.
func TestAgentConnectionHelp_StyledByInstallAll(t *testing.T) {
	withColorDisabled(t)
	t.Chdir(t.TempDir())

	out := helpOf(t, "connection")
	assert.Contains(t, out, "Available Commands:")
	assert.Contains(t, out, "Usage:")
	// The connection root's Available Commands list should include leaf names.
	for _, leaf := range []string{"create", "delete", "list", "show", "update"} {
		assert.True(t, strings.Contains(out, leaf),
			"connection leaf %q missing from Available Commands", leaf)
	}
}
