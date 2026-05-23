// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterConfirmationFlags_AttachesBothFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "leaf"}
	gate := &confirmationGate{}
	registerConfirmationFlags(cmd, gate)

	dryRun := cmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRun, "--dry-run should be registered")
	assert.Equal(t, "false", dryRun.DefValue)

	force := cmd.Flags().Lookup("force")
	require.NotNil(t, force, "--force should be registered")
	assert.Equal(t, "false", force.DefValue)
}

func TestEmitConfirmationEnvelope_Shape(t *testing.T) {
	var buf bytes.Buffer
	err := emitConfirmationEnvelope(&buf, ConfirmationRequest{
		CommandPath:    "agent files delete",
		Description:    "Delete file from a hosted agent.",
		Classification: ConfirmationClassification{Destructive: true},
		Changes:        []string{`Will delete file "report.csv" from agent "my-agent"`},
		ConfirmCommand: "azd ai agent files delete report.csv --force",
	})
	require.NoError(t, err)

	var env ConfirmationEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))

	assert.Equal(t, "confirmation_required", env.Status)
	assert.Equal(t, "agent files delete", env.Command)
	assert.True(t, env.Classification.Destructive)
	assert.False(t, env.Classification.ReadOnly)
	require.Len(t, env.Changes, 1)
	assert.Contains(t, env.Changes[0], "report.csv")
	assert.Contains(t, env.ConfirmCommand, "--force")
}

func TestEmitConfirmationEnvelope_NilChangesBecomesEmptyArray(t *testing.T) {
	// Nil Changes is a callsite mistake but JSON consumers must still see an
	// array (not null) so they can use len() without a nil check.
	var buf bytes.Buffer
	err := emitConfirmationEnvelope(&buf, ConfirmationRequest{
		CommandPath:    "agent invoke",
		ConfirmCommand: "azd ai agent invoke --force",
		Changes:        nil,
	})
	require.NoError(t, err)

	// Match the literal "changes": [] -- not "changes": null.
	assert.Contains(t, buf.String(), `"changes": []`,
		"envelope should emit changes as an empty array when nil")
	assert.NotContains(t, buf.String(), `"changes": null`)
}

func TestRequireConfirmation_ForceProceeds(t *testing.T) {
	outcome, err := requireConfirmation(
		t.Context(),
		nil,
		ConfirmationRequest{CommandPath: "agent invoke"},
		confirmationGate{force: true},
	)
	require.NoError(t, err)
	assert.Equal(t, confirmProceed, outcome)
}

func TestRequireConfirmation_DryRunEmitsEnvelopeAndAborts(t *testing.T) {
	var outcome confirmationOutcome
	data, runErr := captureStdout(t, func() error {
		var innerErr error
		outcome, innerErr = requireConfirmation(
			t.Context(),
			nil,
			ConfirmationRequest{
				CommandPath:    "agent update",
				ConfirmCommand: "azd ai agent update --force",
				Changes:        []string{"Will redeploy agent X"},
			},
			confirmationGate{dryRun: true},
		)
		return innerErr
	})

	require.NoError(t, runErr)
	assert.Equal(t, confirmAbort, outcome)
	assert.Contains(t, data, `"status": "confirmation_required"`)
	assert.Contains(t, data, "agent update")
	assert.Contains(t, data, "--force")
}
