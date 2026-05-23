// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

// Agent-friendly CLI output formats supported across every data-returning
// leaf command in this extension. Keep this in sync with the AllowedValues
// passed to azdext.RegisterFlagOptions.
const (
	// OutputFormatTable is the default human-readable output.
	OutputFormatTable = "table"
	// OutputFormatJSON is the machine-readable output for agents and scripts.
	OutputFormatJSON = "json"
)

// normalizeAgentOutputFormat lowercases the raw value from extCtx.OutputFormat
// so downstream branches can safely compare with `== OutputFormatJSON`. The
// empty string and the SDK's pre-parse sentinel "default" both resolve to the
// human format. The leaf-local normalizeOutputFormat in sample_list.go is
// intentionally separate: that command uses a "text"/"json" enum, not
// "table"/"json".
func normalizeAgentOutputFormat(raw string) string {
	out := strings.ToLower(strings.TrimSpace(raw))
	if out == "" || out == "default" {
		return OutputFormatTable
	}
	return out
}

// validateOutputFormat returns a structured exterrors.Validation when --output
// is not one of the supported values. The azd host normally enforces this via
// RegisterFlagOptions; the explicit check stays for direct `azd x` invocation
// and for unit-test reach.
func validateOutputFormat(out string) error {
	switch normalizeAgentOutputFormat(out) {
	case OutputFormatTable, OutputFormatJSON:
		return nil
	default:
		return exterrors.Validation(
			exterrors.CodeInvalidParameter,
			fmt.Sprintf("invalid --output value %q", out),
			"use table or json",
		)
	}
}

// registerAgentOutputFlag attaches the --output flag annotations every
// data-returning leaf command shares. RegisterFlagOptions writes per-command
// annotations, so it must run on each leaf rather than a parent group.
func registerAgentOutputFlag(cmd *cobra.Command) {
	azdext.RegisterFlagOptions(cmd, azdext.FlagOptions{
		Name:          "output",
		AllowedValues: []string{OutputFormatTable, OutputFormatJSON},
		Default:       OutputFormatTable,
	})
}

// isJSONOutput reports whether the resolved --output value selects JSON.
// Accepts the raw value from extCtx.OutputFormat (case-insensitive).
func isJSONOutput(out string) bool {
	return normalizeAgentOutputFormat(out) == OutputFormatJSON
}

// printJSON serializes v as indented JSON to w with a trailing newline.
// Shared by every command that emits the JSON output format.
func printJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling to JSON: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}
