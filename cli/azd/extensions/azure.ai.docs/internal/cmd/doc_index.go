// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// doc_index.go implements `azd ai doc` (the bare front-door command).
// Lists the sibling ai.* extensions whose docs this extension carries.
// Today only `agent` ships; toolbox/project/training/etc. are added by
// dropping a new subdirectory under internal/cmd/skills/ AND wiring a
// new subcommand in root.go.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// docCategory describes one sibling-extension topic group. SubcommandName
// is the leaf verb under `azd ai doc` (e.g. "agent"). DisplayName is what
// shows in the top-level table.
type docCategory struct {
	SubcommandName string
	DisplayName    string
}

// docCategories lists every sibling-extension topic group whose markdown is
// embedded in this extension. Add a new entry when adding topics for a new
// ai.* extension. The SubcommandName MUST match the directory name under
// internal/cmd/skills/<name>/.
var docCategories = []docCategory{
	{
		SubcommandName: "agent",
		DisplayName:    "Foundry agents (azure.ai.agents)",
	},
}

// runDocIndex is the RunE for the bare `azd ai doc` command. Renders a
// small table of available topic groups. Returns nil so the process exits 0
// even when no categories are registered yet.
func runDocIndex(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Available documentation:")
	fmt.Fprintln(out)
	for _, c := range docCategories {
		fmt.Fprintf(out, "  %-30s azd ai doc %s\n", c.DisplayName, c.SubcommandName)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run `azd ai doc <name>` to list topics, or `azd ai doc <name> <topic>` to print one.")
	return nil
}
