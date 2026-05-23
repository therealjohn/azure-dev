// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAgentOutputFormat(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", OutputFormatTable},
		{"  ", OutputFormatTable},
		{"default", OutputFormatTable},
		{"DEFAULT", OutputFormatTable},
		{" Default ", OutputFormatTable},
		{"table", OutputFormatTable},
		{"TABLE", OutputFormatTable},
		{"json", OutputFormatJSON},
		{"JSON", OutputFormatJSON},
		{"Json", OutputFormatJSON},
		{" json ", OutputFormatJSON},
		{"yaml", "yaml"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, normalizeAgentOutputFormat(tc.in), "input %q", tc.in)
	}
}

func TestValidateOutputFormat_AcceptsSDKDefault(t *testing.T) {
	// SDK substitutes the pre-parse sentinel "default" when no --output is
	// passed; if RegisterFlagOptions does not replace it, the leaf must still
	// accept it without flagging a validation error.
	assert.NoError(t, validateOutputFormat("default"))
	assert.NoError(t, validateOutputFormat("DEFAULT"))
}

func TestValidateOutputFormat_Accepts(t *testing.T) {
	for _, ok := range []string{"", "table", "TABLE", "json", "JSON", " json "} {
		assert.NoError(t, validateOutputFormat(ok), "expected %q to be accepted", ok)
	}
}

func TestValidateOutputFormat_Rejects(t *testing.T) {
	err := validateOutputFormat("yaml")
	require.Error(t, err)
	// The structured error message includes the bad value so users can copy-paste.
	require.True(t, strings.Contains(err.Error(), "yaml"),
		"error %q should reference the bad value", err)
}

func TestIsJSONOutput(t *testing.T) {
	for _, no := range []string{"", "table", "TABLE", "yaml"} {
		assert.False(t, isJSONOutput(no), "input %q should not be JSON", no)
	}
	for _, yes := range []string{"json", "JSON", "Json", " json "} {
		assert.True(t, isJSONOutput(yes), "input %q should be JSON", yes)
	}
}

func TestRegisterAgentOutputFlag_SetsAnnotations(t *testing.T) {
	cmd := &cobra.Command{Use: "leaf"}
	registerAgentOutputFlag(cmd)

	// RegisterFlagOptions records its allowed values and default as cobra
	// annotations under the flag name. The exact annotation key shape is
	// owned by azdext, so we assert presence rather than specific keys —
	// that keeps this test robust if the SDK changes its annotation scheme.
	require.NotEmpty(t, cmd.Annotations,
		"registerAgentOutputFlag should attach SDK annotations to the command")
}
