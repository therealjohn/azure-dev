// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderStarterPrompt_SubstitutesProjectPath(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: "/home/user/my-app",
		SkillPath:   ".claude/skills/azd-ai-skill",
	})
	require.NoError(t, err)
	assert.Contains(t, got, "/home/user/my-app",
		"starter prompt must substitute the ProjectPath into the body")
	assert.Contains(t, got, ".claude/skills/azd-ai-skill",
		"starter prompt must mention the skill install path when one was supplied")
}

func TestRenderStarterPrompt_OmitsInstallClauseWhenSkillPathEmpty(t *testing.T) {
	// Q2=No path: the user declined the skill install, so the prompt
	// must not claim a non-existent install path.
	got, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: "/home/user/my-app",
		SkillPath:   "",
	})
	require.NoError(t, err)
	assert.Contains(t, got, "azd ai",
		"starter prompt must still tell the user to use azd ai when no skill is installed")
	assert.NotContains(t, got, "skill at",
		"starter prompt must not reference a skill install path when SkillPath is empty")
}

func TestRenderStarterPrompt_HasNoTrailingWhitespace(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)
	assert.Equal(t, got, strings.TrimRight(got, " \t\n"), "output should not end with whitespace")
}

// TestRenderStarterPrompt_IncludesCoreInstructions pins the two
// contracts the prompt MUST carry on the user-facing side:
//
//   - tell the agent to use `azd ai` (the skill content handles the
//     rest -- topic chooser, command shapes, envelope mechanics);
//   - the human-readable ask-first contract so the user knows they
//     will be consulted before billing-impacting steps.
//
// We deliberately do NOT pin SKILL.md jargon ("greenfield",
// "manifestUrl", "AZD AI skill", "azd ai doc agent <topic>") here --
// those belong in the skill itself, not in a one-paragraph starter
// prompt the user is about to paste into a coding agent.
func TestRenderStarterPrompt_IncludesCoreInstructions(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)

	wantPhrases := []string{
		"azd ai",
		"Ask me",
	}
	for _, want := range wantPhrases {
		assert.Contains(t, got, want, "starter prompt must mention %q", want)
	}

	// The prompt must NOT chain --from-code after --no-prompt. The skill
	// directs coding agents to pick a curated sample via
	// `azd ai agent sample list` and pass `-m <manifestUrl>` instead;
	// `--from-code` is reserved for brownfield projects. Pin the bad
	// PAIR rather than the flag in isolation so this test still works
	// if `--from-code` is mentioned in passing somewhere.
	assert.NotContains(t, got, "--no-prompt --from-code",
		"starter prompt must NOT instruct the coding agent to chain --from-code after --no-prompt")
}

// TestRenderStarterPrompt_IsBrief pins the simplification at a coarse
// level: a verbose, jargon-heavy starter prompt defeats the purpose of
// installing the AZD AI skill (which already covers commands, flag
// tables, envelope mechanics, and the topic chooser). The agent-
// friendly target is well under 100 words; the cap leaves headroom for
// substituted ProjectPath / SkillPath while still catching regressions.
func TestRenderStarterPrompt_IsBrief(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: "/x",
		SkillPath:   ".claude/skills/azd-ai-skill",
	})
	require.NoError(t, err)
	words := len(strings.Fields(got))
	assert.LessOrEqual(t, words, 100,
		"starter prompt should be a brief statement of intent, not a "+
			"step-by-step recipe; got %d words. If you need to grow it, "+
			"first ask whether the new content belongs in the skill body "+
			"(azure.ai.docs) instead.", words)
}

// mapClipboardEnv is the test-only clipboardEnv: a simple map.
type mapClipboardEnv map[string]string

func (m mapClipboardEnv) Lookup(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

func TestCopyToClipboard_SkipsOnCI(t *testing.T) {
	calls := 0
	write := func(string) error {
		calls++
		return nil
	}
	out := copyToClipboardWith("hello", write, mapClipboardEnv{"CI": "true"})
	assert.Equal(t, ClipboardSkipped, out)
	assert.Equal(t, 0, calls, "clipboard write must not be attempted in CI")
}

func TestCopyToClipboard_SkipsOnTermDumb(t *testing.T) {
	calls := 0
	write := func(string) error {
		calls++
		return nil
	}
	out := copyToClipboardWith("hello", write, mapClipboardEnv{"TERM": "dumb"})
	assert.Equal(t, ClipboardSkipped, out)
	assert.Equal(t, 0, calls)
}

func TestCopyToClipboard_SkipsOnSSH(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY"} {
		t.Run(key, func(t *testing.T) {
			out := copyToClipboardWith(
				"hello",
				func(string) error { t.Fatal("write should not be called"); return nil },
				mapClipboardEnv{key: "1.2.3.4 22 5.6.7.8 22"})
			assert.Equal(t, ClipboardSkipped, out)
		})
	}
}

func TestCopyToClipboard_ReturnsFailedOnWriteError(t *testing.T) {
	// Non-headless env (provide DISPLAY on Linux, leave it untouched
	// on other OSes) -> we attempt the write -> write errors ->
	// outcome is Failed.
	env := mapClipboardEnv{"DISPLAY": ":0"}
	write := func(string) error { return errors.New("no clipboard available") }
	out := copyToClipboardWith("hello", write, env)
	assert.Equal(t, ClipboardFailed, out)
}

func TestCopyToClipboard_CopiesWhenHealthy(t *testing.T) {
	env := mapClipboardEnv{"DISPLAY": ":0"}
	var captured string
	write := func(s string) error {
		captured = s
		return nil
	}
	out := copyToClipboardWith("hello", write, env)
	assert.Equal(t, ClipboardCopied, out)
	assert.Equal(t, "hello", captured)
}

func TestPrintStarterPrompt_IncludesHeaderAndBody(t *testing.T) {
	var buf bytes.Buffer
	printStarterPrompt(&buf, "BODY-MARKER")
	got := buf.String()
	assert.Contains(t, got, "Starter prompt for your AI agent:")
	assert.Contains(t, got, "BODY-MARKER")
}
