// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillsFS_HasFourCanonicalTopics pins the topic set so a future drop
// or rename triggers a deliberate test update -- the topic names are part
// of the wire contract callers in the docs extension rely on.
func TestSkillsFS_HasFourCanonicalTopics(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, listTopics(&buf, "md"))
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.ElementsMatch(t, []string{
		"configure",
		"initialize",
		"investigate",
		"operate",
	}, got)
}

func TestListTopics_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, listTopics(&buf, "json"))
	out := buf.String()
	assert.True(t, strings.Contains(out, `"topics"`), "list JSON missing topics key: %s", out)
	for _, topic := range []string{"initialize", "configure", "investigate", "operate"} {
		assert.True(t, strings.Contains(out, `"`+topic+`"`),
			"list JSON missing topic %q: %s", topic, out)
	}
}

func TestPrintTopic_KnownTopicEmitsBody(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, printTopic(&buf, "initialize", "md"))
	out := buf.String()
	// The initialize topic uses a specific H1 we can pin without coupling
	// to the entire body text.
	assert.True(t, strings.Contains(out, "# Initialize:"),
		"initialize topic body missing expected H1: first 120 chars = %q",
		out[:min(120, len(out))])
}

func TestPrintTopic_KnownTopicJSONShape(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, printTopic(&buf, "configure", "json"))
	out := buf.String()
	assert.True(t, strings.Contains(out, `"topic": "configure"`),
		"json payload missing topic field: %s", out[:min(120, len(out))])
	assert.True(t, strings.Contains(out, `"body":`),
		"json payload missing body field: %s", out[:min(120, len(out))])
}

func TestPrintTopic_UnknownTopicReturnsValidation(t *testing.T) {
	var buf bytes.Buffer
	err := printTopic(&buf, "nonexistent", "md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent",
		"validation error should name the bad topic so the agent can self-correct")
}

func TestNewDocsCommand_IsHidden(t *testing.T) {
	cmd := newDocsCommand()
	// Hidden because the typical entry point for agents is the future
	// azd ai doc agent <topic> command in the azure.ai.docs extension.
	assert.True(t, cmd.Hidden, "azd ai agent docs is intentionally hidden")
}

func TestNewDocsCommand_FlagDefaults(t *testing.T) {
	cmd := newDocsCommand()

	topic := cmd.Flags().Lookup("topic")
	require.NotNil(t, topic, "--topic should be registered")
	assert.Equal(t, "", topic.DefValue, "--topic should default to empty (list mode)")

	out := cmd.Flags().Lookup("output")
	require.NotNil(t, out, "--output should be registered")
	assert.Equal(t, "md", out.DefValue, "--output should default to md")
}
