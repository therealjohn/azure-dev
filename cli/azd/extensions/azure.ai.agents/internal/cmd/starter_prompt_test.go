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
	assert.Contains(t, got, "Initialize a Microsoft Foundry agent in this project at /home/user/my-app.")
	assert.Contains(t, got, "(installed at .claude/skills/azd-ai-skill)")
}

func TestRenderStarterPrompt_OmitsInstallClauseWhenSkillPathEmpty(t *testing.T) {
	// Q2=No path: the user declined the skill install, so the prompt
	// must not claim a non-existent install path.
	got, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: "/home/user/my-app",
		SkillPath:   "",
	})
	require.NoError(t, err)
	assert.Contains(t, got, "Use the AZD AI skill to drive")
	assert.NotContains(t, got, "(installed at")
}

func TestRenderStarterPrompt_HasNoTrailingWhitespace(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)
	assert.Equal(t, got, strings.TrimRight(got, " \t\n"), "output should not end with whitespace")
}

func TestRenderStarterPrompt_IncludesCoreInstructions(t *testing.T) {
	// Pin a handful of phrases the AI agent must see. If the embedded
	// template is accidentally truncated or a step is dropped, this
	// test catches the regression before users do.
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)

	wantPhrases := []string{
		"azd ai agent project show",
		"azd ai agent show --output json",
		"azd provision --no-prompt",
		"azd deploy --no-prompt",
		"--output json",
		"confirmation_required",
		"--force",
	}
	for _, want := range wantPhrases {
		assert.Contains(t, got, want, "starter prompt must mention %q", want)
	}

	// The post-pre-flow scaffold invocation must NOT chain --from-code
	// after --no-prompt. The bootstrap-only state written by the
	// pre-flow is auto-detected by promptInitMode; --from-code is now
	// reserved for the case where the directory ALREADY contains agent
	// code. The prompt may still MENTION the flag (to explain that the
	// agent does not need it for this scenario), so we assert the bad
	// pair specifically rather than the flag name in isolation.
	assert.NotContains(t, got, "--no-prompt --from-code",
		"starter prompt must NOT instruct the coding agent to chain --from-code "+
			"after --no-prompt; auto-detection of the bootstrap stub handles the post-pre-flow case")
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
