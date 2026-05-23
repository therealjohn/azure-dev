// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// optimize_cancel.go implements the "optimize cancel" command, which cancels
// a running optimization job by its operation ID.

package cmd

import (
	"fmt"

	"azureaiagent/internal/pkg/agents/optimize_api"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// optimizeCancelFlags holds connection settings for the cancel command.
type optimizeCancelFlags struct {
	optimizeConnectionFlags
}

func newOptimizeCancelCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	extCtx = ensureExtensionContext(extCtx)
	flags := &optimizeCancelFlags{}
	gate := &confirmationGate{}

	cmd := &cobra.Command{
		Use:   "cancel <operation-id>",
		Short: "Cancel a running optimization job.",
		Long: `Cancel a running optimization or evaluation job by its operation ID.

Only jobs in a non-terminal state (pending, running) can be cancelled.

Confirmation: cancellation is destructive (the job's partial work is lost).
Pass --force to skip the prompt (or the agent confirmation envelope).
Pass --dry-run to preview the cancellation without sending it.`,
		Example: `  # Cancel a running job (prompts to confirm)
  azd ai agent optimize cancel opt_abc123

  # Cancel without prompting
  azd ai agent optimize cancel opt_abc123 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gate.noPrompt = extCtx.NoPrompt
			return runOptimizeCancel(cmd, flags, gate, args[0])
		},
	}

	flags.optimizeConnectionFlags.register(cmd)
	registerConfirmationFlags(cmd, gate)

	return cmd
}

func runOptimizeCancel(cmd *cobra.Command, flags *optimizeCancelFlags, gate *confirmationGate, operationID string) error {
	endpoint, err := flags.resolve(cmd.Context())
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	proceed, err := confirmWrite(ctx, ConfirmationRequest{
		CommandPath:    "agent optimize cancel",
		Description:    fmt.Sprintf("Cancel optimization job %q.", operationID),
		Classification: ConfirmationClassification{Destructive: true},
		Changes: []string{
			fmt.Sprintf("Will request cancellation of optimization job %s; partial work is discarded", operationID),
		},
		ConfirmCommand: fmt.Sprintf("azd ai agent optimize cancel %s --force", operationID),
	}, *gate)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	credential, err := newAgentCredential()
	if err != nil {
		return err
	}

	client := optimize_api.NewOptimizeClient(endpoint, credential)

	cancelResp, err := client.CancelOptimize(ctx, operationID)
	if err != nil {
		return fmt.Errorf("failed to cancel job: %w\n\nCheck that the operation ID %q is correct and the job is still running", err, operationID)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "  %s Job %s has been cancelled (status: %s).\n",
		color.YellowString("⚠"), operationID, cancelResp.Status)
	fmt.Fprintf(out, "\n  Check status with:\n    azd ai agent optimize status %s\n", operationID)

	return nil
}
