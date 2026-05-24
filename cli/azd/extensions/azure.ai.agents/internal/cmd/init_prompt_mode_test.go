// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chdirTo is a small t.Helper that runs the test in a fresh empty dir.
// t.Chdir restores cwd at the end of the test.
func chdirTo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// TestPromptInitMode_FromCodeFlagWins covers routing rule #1: an
// explicit --from-code flag short-circuits everything. The dir is
// non-empty AND not bootstrap-only, which would normally trigger the
// Select prompt; the flag must override that.
func TestPromptInitMode_FromCodeFlagWins(t *testing.T) {
	dir := chdirTo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte("x\n"), 0o644))

	// nil azdClient is safe here because the from-code short-circuit
	// returns before any Prompt RPC is attempted.
	mode, err := promptInitMode(context.Background(), nil, &initFlags{fromCode: true}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, initModeFromCode, mode)
}

// TestPromptInitMode_EmptyDirSelectsTemplate covers routing rule #2.
// This is the legacy behavior preserved for backwards-compatibility:
// no code => offer templates.
func TestPromptInitMode_EmptyDirSelectsTemplate(t *testing.T) {
	_ = chdirTo(t)

	mode, err := promptInitMode(context.Background(), nil, &initFlags{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, initModeTemplate, mode)
}

// TestPromptInitMode_BootstrapOnlyInteractiveRoutesToFromCode covers
// routing rule #3 in interactive mode. The dir contains only the
// pre-flow's stub + a known skill root; we must silently route to
// from-code AND print the muted notice so the user understands why
// the init-mode prompt was skipped.
func TestPromptInitMode_BootstrapOnlyInteractiveRoutesToFromCode(t *testing.T) {
	dir := chdirTo(t)
	stubAzureYaml(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "skills"), 0o755))

	var out bytes.Buffer
	mode, err := promptInitMode(context.Background(), nil, &initFlags{}, &out)
	require.NoError(t, err)
	assert.Equal(t, initModeFromCode, mode,
		"bootstrap-only dirs must route to from-code (NOT template) because "+
			"promptAgentTemplate would also fail under --no-prompt -- see promptInitMode comment")

	assert.Contains(t, out.String(), "Detected AZD AI bootstrap files",
		"interactive mode should print the muted notice so the user knows why "+
			"they did not see the usual init-mode prompt")
}

// TestPromptInitMode_BootstrapOnlyNoPromptStaysSilent covers routing
// rule #3 in non-interactive mode. Same routing decision, but no
// notice is printed -- machine-parsed logs should stay clean.
func TestPromptInitMode_BootstrapOnlyNoPromptStaysSilent(t *testing.T) {
	dir := chdirTo(t)
	stubAzureYaml(t, dir)

	var out bytes.Buffer
	mode, err := promptInitMode(context.Background(), nil, &initFlags{noPrompt: true}, &out)
	require.NoError(t, err)
	assert.Equal(t, initModeFromCode, mode)
	assert.Empty(t, out.String(),
		"--no-prompt mode must not print the muted notice -- it would contaminate machine-parsed logs")
}

// TestPromptInitMode_NonEmptyNonBootstrapNoPromptReturnsSuggestion is
// the headline rubber-duck #4 fix: rather than letting the Select RPC
// fail opaquely in --no-prompt mode, we return an ErrorWithSuggestion
// the coding agent can actually act on.
func TestPromptInitMode_NonEmptyNonBootstrapNoPromptReturnsSuggestion(t *testing.T) {
	dir := chdirTo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte("x\n"), 0o644))

	_, err := promptInitMode(context.Background(), nil, &initFlags{noPrompt: true}, &bytes.Buffer{})
	require.Error(t, err)

	// The suggestion should name BOTH escape hatches so the caller
	// (often a coding agent) can pick the right one. LocalError.Error()
	// returns only Message, so we assert on the Suggestion field
	// directly via the structured error.
	localErr, ok := errors.AsType[*azdext.LocalError](err)
	require.True(t, ok, "expected *azdext.LocalError, got %T: %v", err, err)

	assert.Contains(t, localErr.Suggestion, "--from-code",
		"suggestion should mention --from-code as the 'use existing code' escape hatch")
	assert.Contains(t, localErr.Suggestion, "--manifest",
		"suggestion should mention --manifest as the 'pick an agent template' escape hatch")

	assert.Equal(t, exterrors.CodePromptFailed, localErr.Code,
		"non-interactive init-mode failure should be tagged with CodePromptFailed for telemetry")
	assert.Equal(t, azdext.LocalErrorCategoryValidation, localErr.Category,
		"the failure is a user-input issue, not a dependency or auth failure")
}

// TestPromptInitMode_PropagatesBootstrapDetectionError pins
// rubber-duck #7: a filesystem error from dirIsAgentBootstrapOnly
// must surface, not silently degrade to "false" which would then
// route the user through the wrong-shaped prompt with no diagnostic.
func TestPromptInitMode_PropagatesBootstrapDetectionError(t *testing.T) {
	if testing.Short() {
		// Permission-flip is slow on some CI setups; skip in -short.
		t.Skip("skip in short mode")
	}
	if !canMakeDirUnreadable() {
		t.Skip("read-only directory permissions are not enforced on this OS")
	}

	dir := chdirTo(t)
	// stub azure.yaml present so dirIsEmpty returns false and we land
	// in the bootstrap-detection branch.
	stubAzureYaml(t, dir)
	// Add an unreadable subdir to force a walk error inside
	// dirIsAgentBootstrapOnly's SKILL.md probe.
	subdir := filepath.Join(dir, "unknown-dir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chmod(subdir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(subdir, 0o755) })

	_, err := promptInitMode(context.Background(), nil, &initFlags{noPrompt: true}, &bytes.Buffer{})
	if err == nil {
		// If our chmod was a no-op (e.g. running as root in a container),
		// skip rather than fail: this assertion is about ERROR PROPAGATION,
		// not about whether THIS specific chmod trick blocks the walk.
		t.Skip("could not produce a filesystem error to propagate (likely running as root)")
	}
	assert.True(t,
		strings.Contains(err.Error(), "bootstrap") ||
			strings.Contains(err.Error(), "walk"),
		"propagated error should mention the failing operation; got %v", err)
}

// canMakeDirUnreadable reports whether chmod 0o000 on a dir actually
// denies reads on this OS. False on Windows, where ACLs are the real
// access control mechanism and POSIX-mode chmod is largely cosmetic.
func canMakeDirUnreadable() bool {
	dir, err := os.MkdirTemp("", "chmod-probe-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o000); err != nil {
		return false
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck
	_, err = os.ReadDir(dir)
	return err != nil
}
