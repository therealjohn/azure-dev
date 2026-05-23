// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// ext_lookup.go provides helpers for talking to the azd host's extension
// layer from inside this extension. Two responsibilities:
//
//  1. Detect whether a sibling extension (e.g. azure.ai.docs) is
//     installed locally so a cross-extension dispatch is safe.
//  2. Run a child `azd` subprocess to invoke another extension's
//     command (skill install, ext install, etc.).
//
// Both helpers shell out to `azd` because the gRPC SDK does not (yet)
// expose extension-management RPCs from inside an extension. Pattern
// matches the existing exec.Command("azd", ...) sites in
// microsoft.azd.extensions and microsoft.azd.concurx.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// extListItem mirrors the wire shape emitted by `azd ext list -o json`.
// Only the fields we need are decoded; the SDK adds extra fields freely.
type extListItem struct {
	ID               string `json:"id"`
	Namespace        string `json:"namespace"`
	InstalledVersion string `json:"installedVersion"`
}

// extLookup describes the install state of one sibling extension. The
// shape stays small on purpose -- callers only need to know "is it
// installed?" and "what's the namespace I'd invoke?" (for nicer error
// messages when the answer is no).
type extLookup struct {
	ID        string
	Namespace string
	Installed bool
}

// azdRunner abstracts the exec.Command wiring so tests can inject a
// fake. Default production runner is osAzdRunner below.
type azdRunner interface {
	// Run executes `azd <args...>` with the given stdout/stderr writers
	// and returns the process error (nil on exit 0). Cancellation is
	// honored when ctx is canceled.
	Run(ctx context.Context, args []string, stdout, stderr io.Writer) error
	// Output executes `azd <args...>` and returns combined stdout +
	// error (mirrors exec.Command.Output). Used by the JSON-parsing
	// helpers where streaming is not needed.
	Output(ctx context.Context, args []string) ([]byte, error)
}

// osAzdRunner is the default production runner.
type osAzdRunner struct{}

func (osAzdRunner) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "azd", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (osAzdRunner) Output(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "azd", args...)
	return cmd.Output()
}

// lookupExtension returns the install state for the given extension ID.
// Returns (lookup, nil) when the listing succeeds and the ID is found;
// returns (lookup with Installed=false, nil) when listing succeeds and
// the ID is missing; returns (zero, err) when the listing itself fails.
func lookupExtension(ctx context.Context, runner azdRunner, id string) (extLookup, error) {
	out, err := runner.Output(ctx, []string{"ext", "list", "-o", "json"})
	if err != nil {
		return extLookup{}, fmt.Errorf("run `azd ext list -o json`: %w", err)
	}

	var items []extListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return extLookup{}, fmt.Errorf("parse `azd ext list` output: %w", err)
	}

	for _, it := range items {
		if !strings.EqualFold(it.ID, id) {
			continue
		}
		return extLookup{
			ID:        it.ID,
			Namespace: it.Namespace,
			Installed: strings.TrimSpace(it.InstalledVersion) != "",
		}, nil
	}

	// Not present in the catalog at all (no registry source advertises
	// it, or the user has not added the right source). Return a lookup
	// with Installed=false so the caller surfaces an "install it" hint.
	return extLookup{ID: id, Installed: false}, nil
}

// installExtension shells out to `azd ext install <id>`. Streams output
// through stdout/stderr so the user sees install progress live. Used
// when the user opts in to auto-installing a missing dependency.
func installExtension(ctx context.Context, runner azdRunner, id string, stdout, stderr io.Writer) error {
	args := []string{"ext", "install", id}
	if err := runner.Run(ctx, args, stdout, stderr); err != nil {
		return fmt.Errorf("install extension %q: %w", id, err)
	}
	return nil
}

// runChildAzd invokes `azd <args...>` with stdout/stderr streamed
// through. Returns the process error verbatim so the caller can pattern-
// match on exit codes / unwrap exec.ExitError when needed.
//
// Used by the init pre-flow to dispatch `azd ai doc skills install`.
// Always pass --no-prompt + --output json from the caller; this helper
// makes no assumption about flags so it can be reused for other
// cross-extension calls in the future.
func runChildAzd(ctx context.Context, runner azdRunner, args []string, stdout, stderr io.Writer) error {
	return runner.Run(ctx, args, stdout, stderr)
}

// childAzdMissingError reports whether err looks like the azd child
// failed because the target command/subcommand does not exist (the
// extension was not installed, or its namespace is wrong). Used by the
// pre-flow to convert opaque cobra "unknown command" errors into a
// clear "install the docs extension" hint.
func childAzdMissingError(err error) bool {
	if err == nil {
		return false
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	stderr := strings.ToLower(string(ee.Stderr))
	return strings.Contains(stderr, "unknown command") ||
		strings.Contains(stderr, "unknown flag") ||
		strings.Contains(stderr, "not found")
}

// defaultAzdRunner is the package-level production runner. Tests
// construct their own runner and call the *With helpers directly.
var defaultAzdRunner azdRunner = osAzdRunner{}

// Discard is a convenience io.Writer for callers that only need the
// exit code from a child azd run and intentionally drop its output.
// Mirrors io.Discard but kept package-local so future changes to the
// silenced-output story (e.g. capture + log on failure) live in one
// place.
var Discard io.Writer = io.Discard

// dropOSEnv is used by callers that want to stream output to the
// terminal rather than a custom writer. Returns the calling process's
// stdout/stderr so the child's output reaches the user directly.
func defaultOutputs() (io.Writer, io.Writer) {
	return os.Stdout, os.Stderr
}
