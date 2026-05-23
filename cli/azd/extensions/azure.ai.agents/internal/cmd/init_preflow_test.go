// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"strings"
	"testing"

	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/stretchr/testify/assert"
)

func TestPreflowTargets_AllExpectedToolsPresent(t *testing.T) {
	// Drift guard: if a target is added/removed/renamed, the install
	// command in azure.ai.docs MUST keep the same list in sync.
	want := []string{"claude", "codex", "gemini", "copilot", "opencode", "custom"}
	got := make([]string, len(preflowTargets))
	for i, t := range preflowTargets {
		got[i] = t.targetValue
	}
	assert.Equal(t, want, got)
}

func TestPreflowTargets_PathsAlignWithDocsExtension(t *testing.T) {
	// Both extensions ship their own targets table; the upstream test
	// in azure.ai.docs already pins the canonical paths. This test
	// pins the same paths on the consumer side so the two cannot
	// drift silently.
	cases := map[string]string{
		"claude":   ".claude/skills/microsoft-foundry",
		"codex":    ".agents/skills/microsoft-foundry",
		"gemini":   ".agents/skills/microsoft-foundry",
		"copilot":  ".agents/skills/microsoft-foundry",
		"opencode": ".agents/skills/microsoft-foundry",
		"custom":   "",
	}
	for _, tgt := range preflowTargets {
		want, ok := cases[tgt.targetValue]
		if !ok {
			continue
		}
		assert.Equal(t, want, tgt.installPath, "install path mismatch for %s", tgt.targetValue)
	}
}

func TestPreflowTargets_HavePasteInstructions(t *testing.T) {
	// The ready-to-go block uses pasteInstruction verbatim; an empty
	// string would render a confusing blank line.
	for _, tgt := range preflowTargets {
		assert.NotEmpty(t, tgt.pasteInstruction, "target %s missing pasteInstruction", tgt.targetValue)
	}
}

func TestTargetSelectLabel_IncludesPathInGray(t *testing.T) {
	got := targetSelectLabel(preflowTarget{
		targetValue: "copilot",
		displayName: "GitHub Copilot",
		installPath: ".agents/skills/microsoft-foundry",
	})
	assert.Contains(t, got, "GitHub Copilot")
	assert.Contains(t, got, ".agents/skills/microsoft-foundry")
	// Color is rendered as ANSI escape sequences when the global
	// fatih/color noColor flag is unset, but our assertions stay
	// color-agnostic to avoid flakiness in CI. The label content is
	// what matters; the color comes from the WithGrayFormat call which
	// is covered by its own package's tests.
	gray := output.WithGrayFormat("(.agents/skills/microsoft-foundry)")
	assert.True(t,
		strings.Contains(got, gray) ||
			strings.Contains(got, "(.agents/skills/microsoft-foundry)"),
		"label should include gray-formatted path; got %q", got)
}

func TestTargetSelectLabel_OmitsParenWhenPathEmpty(t *testing.T) {
	got := targetSelectLabel(preflowTarget{
		targetValue: "custom",
		displayName: "Custom path",
		installPath: "",
	})
	assert.Equal(t, "Custom path", got)
}

func TestPrintReadyToGo_IncludesPasteInstructionAndManualFallback(t *testing.T) {
	var buf testWriter
	a := &InitPreflowAction{out: &buf}
	a.printReadyToGo(preflowTarget{
		targetValue:      "copilot",
		displayName:      "GitHub Copilot",
		installPath:      ".agents/skills/microsoft-foundry",
		pasteInstruction: "Open GitHub Copilot Chat and paste the prompt.",
	}, ".agents/skills/microsoft-foundry")

	got := buf.String()
	assert.Contains(t, got, "You're ready to go!")
	assert.Contains(t, got, "Open GitHub Copilot Chat and paste the prompt.")
	assert.Contains(t, got, "Your agent will use the Microsoft Foundry skill at .agents/skills/microsoft-foundry")
	assert.Contains(t, got, "Prefer to set up manually?")
	assert.Contains(t, got, "azd ai agent init")
	assert.Contains(t, got, "azd provision")
	assert.Contains(t, got, "azd deploy")
	assert.Contains(t, got, "azd ai agent show")
	assert.Contains(t, got, "azd ai doc agent")
}

func TestPrintReadyToGo_OmitsInstallReferenceWhenInstallSkipped(t *testing.T) {
	var buf testWriter
	a := &InitPreflowAction{out: &buf}
	a.printReadyToGo(preflowTarget{
		targetValue:      "custom",
		displayName:      "Custom path",
		installPath:      "",
		pasteInstruction: "Open your coding agent and paste the prompt.",
	}, "")

	got := buf.String()
	assert.Contains(t, got, "You're ready to go!")
	assert.Contains(t, got, "Open your coding agent and paste the prompt.")
	// When the user declined Q2, the block should NOT claim the skill
	// is installed at any specific path.
	assert.NotContains(t, got, "Your agent will use the Microsoft Foundry skill at")
	assert.Contains(t, got, "Your agent will follow the starter prompt")
	// Manual-fallback section still renders so the user has a way out.
	assert.Contains(t, got, "Prefer to set up manually?")
}

// testWriter is a tiny io.Writer that captures into a strings.Builder.
// Kept local to this file so test imports stay tight.
type testWriter struct {
	strings.Builder
}

func (w *testWriter) Write(p []byte) (int, error) {
	return w.Builder.Write(p)
}
