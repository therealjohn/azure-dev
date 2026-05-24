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
	// Pin a handful of phrases the AI agent must see. The simplified
	// prompt is a brief statement of intent, NOT a step-by-step recipe
	// -- step-level commands live in the topic bodies the agent pulls
	// via `azd ai doc agent <topic>`. We only pin the contracts the
	// prompt itself MUST carry:
	//
	//   * the AZD AI skill reference (so the agent knows where to read),
	//   * the envelope contract (so the agent never auto-approves),
	//   * the topic-routing call (so the agent loads details on demand).
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)

	wantPhrases := []string{
		"AZD AI skill",
		"azd ai doc agent",
		"confirmation_required",
	}
	for _, want := range wantPhrases {
		assert.Contains(t, got, want, "starter prompt must mention %q", want)
	}

	// The prompt must NOT chain --from-code after --no-prompt. The
	// bootstrap-only state written by the pre-flow is auto-detected
	// by promptInitMode; --from-code is now reserved for the case
	// where the directory ALREADY contains agent code. The prompt
	// may still mention `--from-code` in passing, so we assert the
	// bad PAIR specifically rather than the flag in isolation.
	assert.NotContains(t, got, "--no-prompt --from-code",
		"starter prompt must NOT instruct the coding agent to chain --from-code "+
			"after --no-prompt; auto-detection of the bootstrap stub handles the post-pre-flow case")
}

// TestRenderStarterPrompt_PointsAtTopicVerbs pins the simplification:
// the prompt MUST route the AI agent into `azd ai doc agent <topic>`
// for deep guidance instead of duplicating every flag table inline.
// Topic names match the verbs shipped by the azure.ai.docs extension
// (samples / initialize / develop / configure / extend / deploy /
// evaluate / operate / investigate). If any topic is renamed or
// removed, this test catches the drift before users do.
func TestRenderStarterPrompt_PointsAtTopicVerbs(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{ProjectPath: "/x"})
	require.NoError(t, err)

	// The prompt must instruct the AI agent to pull the topic body.
	assert.Contains(t, got, "azd ai doc agent",
		"starter prompt must direct the agent at `azd ai doc agent <topic>` "+
			"so deeper guidance is loaded on demand instead of duplicated inline")

	// The prompt must reference the high-traffic verbs (initialize,
	// develop, deploy, operate, investigate) so the agent knows which
	// topics back the standard journey.
	wantTopics := []string{
		"initialize",
		"develop",
		"deploy",
		"operate",
		"investigate",
	}
	for _, want := range wantTopics {
		assert.Contains(t, got, want,
			"starter prompt must reference topic verb %q", want)
	}
}

// TestRenderStarterPrompt_IsBrief pins the simplification at a coarse
// level: a verbose step-by-step prompt (where the AI agent re-reads
// flags + envelope mechanics + journey order inline every time) defeats
// the purpose of installing the AZD AI skill. The agent-friendly
// reference target is ~70-100 words; we cap the prompt at 200 words to
// leave headroom for substituted ProjectPath / SkillPath while still
// catching regressions.
func TestRenderStarterPrompt_IsBrief(t *testing.T) {
	got, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: "/x",
		SkillPath:   ".claude/skills/azd-ai-skill",
	})
	require.NoError(t, err)
	words := len(strings.Fields(got))
	assert.LessOrEqual(t, words, 200,
		"starter prompt should be a brief statement of intent, not a "+
			"step-by-step recipe; got %d words. If you need to grow it, "+
			"first ask whether the new content belongs in a topic body "+
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
