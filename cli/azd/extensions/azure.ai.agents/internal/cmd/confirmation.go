// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

// ExitCodeConfirmationRequired is the process exit code emitted when an agent
// runs a write command without --force and no human is available to confirm.
// Follows the convention of exit 2 + confirmation_required envelope and
// is documented in the extension README.
const ExitCodeConfirmationRequired = 2

// ConfirmationEnvelope is the JSON payload returned to agents when a write
// command would mutate state but no --force was supplied. JSON tags are part
// of the extension's documented contract -- do not rename without a deprecation.
type ConfirmationEnvelope struct {
	Status         string                     `json:"status"`         // always "confirmation_required"
	Command        string                     `json:"command"`        // e.g. "agent update"
	Description    string                     `json:"description"`    // human-readable summary
	Classification ConfirmationClassification `json:"classification"` // mutation shape hints
	Changes        []string                   `json:"changes"`        // bullet list of what will happen
	ConfirmCommand string                     `json:"confirmCommand"` // exact command to re-run with --force
}

// ConfirmationClassification describes the shape of a pending write so agents
// can decide how much friction to add to the human confirmation step.
type ConfirmationClassification struct {
	ReadOnly    bool `json:"readOnly"`    // always false for write commands
	Destructive bool `json:"destructive"` // delete/cancel/destroy operations
	Idempotent  bool `json:"idempotent"`  // re-running is safe and a no-op
}

// confirmationStatusRequired is the wire value for Status when human approval
// is needed. Centralized as a constant so callers don't typo it.
const confirmationStatusRequired = "confirmation_required"

// ConfirmationRequest describes a pending write so requireConfirmation can
// build the envelope, render a human prompt, or emit a dry-run preview.
type ConfirmationRequest struct {
	// CommandPath is the leaf command identifier without the "azd ai " prefix
	// (e.g. "agent update", "agent files delete"). Used in the envelope's
	// `command` field and in the confirmation prompt.
	CommandPath string
	// Description is a single-line human-readable summary of the operation.
	Description string
	// Classification hints at the mutation shape.
	Classification ConfirmationClassification
	// Changes is the bullet list of concrete changes the command will make.
	// Render-ready strings -- no leading bullet character.
	Changes []string
	// ConfirmCommand is the exact command (with --force) the agent must run
	// after the human confirms. Callers MUST include `--force` and any flags
	// the agent originally passed so the re-run produces the same result.
	ConfirmCommand string
}

// confirmationOutcome reports what the caller should do after requireConfirmation.
type confirmationOutcome int

const (
	// confirmProceed means continue with the write.
	confirmProceed confirmationOutcome = iota
	// confirmAbort means stop without erroring. The helper already wrote any
	// envelope or cancellation message. Callers should return nil so the
	// extension exits 0 (dry-run) or so cobra emits no extra error text.
	confirmAbort
)

// confirmationGate carries the cross-cutting flags every write command exposes.
// Use registerConfirmationFlags to attach --dry-run and --force consistently.
type confirmationGate struct {
	force    bool // skip the prompt/envelope and proceed
	dryRun   bool // emit envelope and exit 0 without mutating
	noPrompt bool // refuse prompts; emit envelope and exit 2 (agent mode)
}

// registerConfirmationFlags wires --dry-run and --force on every write leaf.
// Keep the flag names consistent across commands so agent documentation only
// has to explain them once.
func registerConfirmationFlags(cmd *cobra.Command, gate *confirmationGate) {
	cmd.Flags().BoolVar(&gate.dryRun, "dry-run", false,
		"Print the envelope describing what would happen and exit without mutating state.")
	cmd.Flags().BoolVar(&gate.force, "force", false,
		"Skip the confirmation prompt or envelope and proceed immediately.")
}

// newConfirmationClient is a small wrapper that returns a short-lived azd
// client used purely by requireConfirmation to render the interactive prompt.
// Callsites use the returned cleanup func in a defer. nil-safe: when client
// creation fails, requireConfirmation falls back to the agent-mode envelope
// path so the user still gets a useful structured response.
func newConfirmationClient() (*azdext.AzdClient, func()) {
	c, err := azdext.NewAzdClient()
	if err != nil {
		return nil, func() {}
	}
	return c, func() { c.Close() }
}

// requireConfirmation gates a write command on user confirmation.
//
// Behavior matrix (--force wins over --dry-run wins over --no-prompt):
//   - --force true:         confirmProceed (caller continues immediately).
//   - --dry-run true:       envelope written to stdout, returns confirmAbort.
//     Caller should return nil so the process exits 0.
//   - --no-prompt true:     envelope written to stdout, calls os.Exit(2).
//     Defers don't run; the envelope is the final output.
//   - interactive (none):   prompts the human via the azd host;
//     yes -> confirmProceed, no -> confirmAbort.
//
// When azdClient is nil (rare; only when extension is running outside the azd
// host), the helper treats the environment as non-interactive and emits the
// agent envelope so the caller gets a useful structured response.
func requireConfirmation(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	req ConfirmationRequest,
	gate confirmationGate,
) (confirmationOutcome, error) {
	// Highest precedence: explicit --force is the agent's signed approval.
	if gate.force {
		return confirmProceed, nil
	}

	// --dry-run prints the envelope and exits the command cleanly with 0.
	// Agent and human callers both get the same payload to inspect before
	// running with --force.
	if gate.dryRun {
		if err := emitConfirmationEnvelope(os.Stdout, req); err != nil {
			return confirmAbort, err
		}
		return confirmAbort, nil
	}

	// Agent/no-prompt mode without --force: emit the envelope and exit 2 so
	// the caller (agent) presents the changes to the human and re-invokes
	// with --force. os.Exit(2) skips defers; the envelope is the only output
	// the agent reads from this run.
	if gate.noPrompt || azdClient == nil {
		_ = emitConfirmationEnvelope(os.Stdout, req)
		os.Exit(ExitCodeConfirmationRequired)
		return confirmAbort, nil // unreachable
	}

	// Interactive path: ask the azd host for a yes/no.
	return interactiveConfirm(ctx, azdClient, req)
}

// confirmWrite is the convenience wrapper most write-command call sites
// should use. Handles azd-client lifecycle and translates requireConfirmation's
// outcome into a (proceed bool, err error) pair so callers can write:
//
//	proceed, err := confirmWrite(ctx, ConfirmationRequest{...}, gate)
//	if err != nil { return err }
//	if !proceed { return nil }
//	// ... continue with the write
func confirmWrite(ctx context.Context, req ConfirmationRequest, gate confirmationGate) (bool, error) {
	confirmClient, cleanup := newConfirmationClient()
	defer cleanup()

	outcome, err := requireConfirmation(ctx, confirmClient, req, gate)
	if err != nil {
		return false, err
	}
	return outcome == confirmProceed, nil
}

// emitConfirmationEnvelope marshals the envelope to JSON and writes it to w.
// Always returns a structured exterrors.Internal on marshal failure so the
// downstream cobra/azdext layer produces a useful error trail.
func emitConfirmationEnvelope(w io.Writer, req ConfirmationRequest) error {
	env := ConfirmationEnvelope{
		Status:         confirmationStatusRequired,
		Command:        req.CommandPath,
		Description:    req.Description,
		Classification: req.Classification,
		Changes:        req.Changes,
		ConfirmCommand: req.ConfirmCommand,
	}
	if env.Changes == nil {
		// Always emit an empty array rather than null so JSON consumers can
		// rely on len(changes) without first checking for nil.
		env.Changes = []string{}
	}
	if err := printJSON(w, env); err != nil {
		return exterrors.Internal(
			"confirmation_envelope_marshal_failed",
			fmt.Sprintf("marshal confirmation envelope: %s", err),
		)
	}
	return nil
}

// interactiveConfirm renders the changes as a numbered list and asks the
// azd host for a yes/no. On yes returns confirmProceed; on no returns
// confirmAbort with a brief stderr notice (no error -- cancellation is
// intentional).
func interactiveConfirm(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	req ConfirmationRequest,
) (confirmationOutcome, error) {
	if len(req.Changes) > 0 {
		fmt.Fprintln(os.Stderr, req.Description)
		fmt.Fprintln(os.Stderr, "Will:")
		for _, c := range req.Changes {
			fmt.Fprintf(os.Stderr, "  - %s\n", c)
		}
	}

	resp, err := azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
		Options: &azdext.ConfirmOptions{
			Message:      "Continue?",
			DefaultValue: new(false),
		},
	})
	if err != nil {
		return confirmAbort, exterrors.FromPrompt(err, "asking for confirmation")
	}
	if resp.Value == nil || !*resp.Value {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		return confirmAbort, nil
	}
	return confirmProceed, nil
}
