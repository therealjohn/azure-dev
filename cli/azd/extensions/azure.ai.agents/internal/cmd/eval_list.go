// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// eval_list.go implements the "eval list" command, which lists recent
// evaluations for the current Foundry project with run counts and status.

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"text/tabwriter"

	"azureaiagent/internal/pkg/agents/eval_api"
	"azureaiagent/internal/pkg/agents/opt_eval"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

// evalListFlags holds CLI flags for the eval list command.
type evalListFlags struct {
	limit           int    // maximum number of evals to return
	output          string // table or json
	noPrompt        bool   // refuse to prompt the user
	projectEndpoint string // explicit project endpoint override
}

func newEvalListCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	extCtx = ensureExtensionContext(extCtx)
	flags := &evalListFlags{limit: 10}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluations for the current project.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := azdext.WithAccessToken(cmd.Context())
			logCleanup := setupDebugLogging(cmd.Flags())
			defer logCleanup()
			flags.output = extCtx.OutputFormat
			flags.noPrompt = extCtx.NoPrompt
			if err := validateOutputFormat(flags.output); err != nil {
				return err
			}
			return runEvalList(ctx, flags)
		},
	}
	cmd.Flags().IntVar(&flags.limit, "limit", 10, "Maximum number of evals to return")
	cmd.Flags().StringVarP(&flags.projectEndpoint, "project-endpoint", "p", "",
		"Foundry project endpoint URL (overrides env var and config)")
	registerAgentOutputFlag(cmd)
	return cmd
}

// evalRunSummary holds the fetched run info for a single eval.
type evalRunSummary struct {
	runCount      int
	lastRunStatus string
}

func runEvalList(ctx context.Context, flags *evalListFlags) error {
	resolved, err := resolveEvalContext(ctx, evalContextOptions{
		noPrompt:        flags.noPrompt,
		projectEndpoint: flags.projectEndpoint,
		quiet:           isJSONOutput(flags.output),
	})
	if err != nil {
		return err
	}
	defer resolved.azdClient.Close()

	// Load the active eval ID from the azd environment.
	var activeEvalID string
	if resolved.envName != "" {
		state := opt_eval.LoadEvalState(ctx, resolved.azdClient, resolved.envName)
		activeEvalID = state.EvalID
	}

	resp, err := resolved.evalClient.ListOpenAIEvals(ctx, flags.limit, DefaultAgentAPIVersion)
	if err != nil {
		return fmt.Errorf("failed to list evals: %w", err)
	}

	items := resp.Data

	// Fetch run summaries in parallel for each eval, bounded by a semaphore
	// to avoid overwhelming the service with concurrent requests.
	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	summaries := make([]evalRunSummary, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, evalID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runs, err := resolved.evalClient.ListOpenAIEvalRuns(ctx, evalID, 10, DefaultAgentAPIVersion)
			if err != nil || runs == nil {
				return
			}
			summaries[idx].runCount = len(runs.Data)
			if len(runs.Data) > 0 {
				summaries[idx].lastRunStatus = runs.Data[0].Status
			}
		}(i, item.ResolvedID())
	}
	wg.Wait()

	if isJSONOutput(flags.output) {
		return printEvalListJSON(os.Stdout, items, summaries, activeEvalID)
	}
	return printEvalListTable(items, summaries, activeEvalID, flags.limit)
}

// evalListItem is the JSON shape for a single eval row in `azd ai agent eval list --output json`.
type evalListItem struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Active        bool   `json:"active"`
	RunCount      int    `json:"runCount"`
	LastRunStatus string `json:"lastRunStatus,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	// CreatedAt mirrors the upstream OpenAIEval.CreatedAt shape (any: int64,
	// float64, or string depending on the source). Keep it as any so JSON
	// consumers see the same value that came from the service.
	CreatedAt any `json:"createdAt,omitempty"`
}

// evalListResponse wraps the items so the JSON output is an object (extensible
// for future fields like pagination cursors) rather than a bare array.
type evalListResponse struct {
	Items []evalListItem `json:"items"`
}

func printEvalListJSON(w io.Writer, items []eval_api.OpenAIEval, summaries []evalRunSummary, activeEvalID string) error {
	out := evalListResponse{Items: make([]evalListItem, 0, len(items))}
	for i, item := range items {
		id := item.ResolvedID()
		out.Items = append(out.Items, evalListItem{
			ID:            id,
			Name:          item.Name,
			Active:        id == activeEvalID,
			RunCount:      summaries[i].runCount,
			LastRunStatus: summaries[i].lastRunStatus,
			CreatedBy:     item.CreatedBy,
			CreatedAt:     item.CreatedAt,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling eval list to JSON: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

func printEvalListTable(items []eval_api.OpenAIEval, summaries []evalRunSummary, activeEvalID string, limit int) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  \tEval ID\tName\tStatus of last run\tRuns\tCreated by\tCreated on")
	fmt.Fprintln(w, "  \t-------\t----\t------------------\t----\t----------\t----------")
	for i, item := range items {
		marker := " "
		if item.ResolvedID() == activeEvalID {
			marker = "*"
		}
		name := item.Name
		if name == "" {
			name = item.ResolvedID()
		}
		status := padColorizedStatus(summaries[i].lastRunStatus)
		createdBy := item.CreatedBy
		createdOn := eval_api.FormatTimestamp(item.CreatedAt)

		fmt.Fprintf(w, "%s \t%s\t%s\t%s\t%d\t%s\t%s\n",
			marker,
			item.ResolvedID(),
			name,
			status,
			summaries[i].runCount,
			createdBy,
			createdOn,
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if activeEvalID != "" {
		fmt.Printf("\n* = active eval in current environment\n")
	}
	fmt.Printf("(showing %d — use --limit to change)\n", len(items))
	return nil
}
