// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillsFS_HasFourCanonicalTopics pins the topic set so a future drop
// or rename triggers a deliberate test update -- the topic names are part
// of the wire contract callers in the docs extension rely on.
func TestSkillsFS_HasFourCanonicalTopics(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, listTopics(&buf, docsOutputMarkdown))
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
	require.NoError(t, listTopics(&buf, docsOutputJSON))
	out := buf.String()
	assert.True(t, strings.Contains(out, `"topics"`), "list JSON missing topics key: %s", out)
	for _, topic := range []string{"initialize", "configure", "investigate", "operate"} {
		assert.True(t, strings.Contains(out, `"`+topic+`"`),
			"list JSON missing topic %q: %s", topic, out)
	}
}

func TestPrintTopic_KnownTopicEmitsBody(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, printTopic(&buf, "initialize", docsOutputMarkdown))
	out := buf.String()
	// The initialize topic uses a specific H1 we can pin without coupling
	// to the entire body text.
	assert.True(t, strings.Contains(out, "# Initialize:"),
		"initialize topic body missing expected H1: first 120 chars = %q",
		out[:min(120, len(out))])
}

func TestPrintTopic_KnownTopicJSONShape(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, printTopic(&buf, "configure", docsOutputJSON))
	out := buf.String()
	assert.True(t, strings.Contains(out, `"topic": "configure"`),
		"json payload missing topic field: %s", out[:min(120, len(out))])
	assert.True(t, strings.Contains(out, `"body":`),
		"json payload missing body field: %s", out[:min(120, len(out))])
}

func TestPrintTopic_UnknownTopicReturnsValidation(t *testing.T) {
	var buf bytes.Buffer
	err := printTopic(&buf, "nonexistent", docsOutputMarkdown)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent",
		"validation error should name the bad topic so the agent can self-correct")
}

func TestNewDocsCommand_IsHidden(t *testing.T) {
	cmd := newDocsCommand(&azdext.ExtensionContext{})
	// Hidden because the typical entry point for agents is the future
	// azd ai doc agent <topic> command in the azure.ai.docs extension.
	assert.True(t, cmd.Hidden, "azd ai agent docs is intentionally hidden")
}

func TestNewDocsCommand_DoesNotRegisterReservedOutputFlag(t *testing.T) {
	// The azd host reserves --output as a global. Extension commands MUST
	// route through azdext.RegisterFlagOptions (which annotates the
	// allowed values + default for help-text purposes only) rather than
	// adding their own local --output flag, which the host would reject
	// with a "conflicts with reserved global flag" error at boot.
	cmd := newDocsCommand(&azdext.ExtensionContext{})
	out := cmd.Flags().Lookup("output")
	assert.Nil(t, out,
		"--output must NOT be locally registered; use azdext.RegisterFlagOptions instead")
}

func TestNewDocsCommand_HasTopicFlag(t *testing.T) {
	cmd := newDocsCommand(&azdext.ExtensionContext{})
	topic := cmd.Flags().Lookup("topic")
	require.NotNil(t, topic, "--topic should be registered")
	assert.Equal(t, "", topic.DefValue, "--topic should default to empty (list mode)")
}

func TestValidateDocsOutputFormat_AcceptsExpected(t *testing.T) {
	for _, ok := range []string{"", "default", "md", "MD", "json", "JSON"} {
		assert.NoError(t, validateDocsOutputFormat(ok), "expected %q to be accepted", ok)
	}
}

func TestValidateDocsOutputFormat_RejectsTableAndOthers(t *testing.T) {
	// "table" is a valid value for OTHER agent commands but not for docs --
	// docs returns markdown by default, not a tabular view of topic names.
	for _, bad := range []string{"table", "yaml", "text"} {
		err := validateDocsOutputFormat(bad)
		require.Error(t, err, "%q should be rejected", bad)
		assert.Contains(t, err.Error(), bad, "error should name the bad value")
	}
}

func TestIsDocsJSON(t *testing.T) {
	for _, no := range []string{"", "default", "md", "MD", "yaml"} {
		assert.False(t, isDocsJSON(no), "input %q should not be JSON", no)
	}
	for _, yes := range []string{"json", "JSON", " json "} {
		assert.True(t, isDocsJSON(yes), "input %q should be JSON", yes)
	}
}
