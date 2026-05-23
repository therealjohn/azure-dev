// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// doc_agent.go implements `azd ai doc agent [topic]` -- the leaf that
// routes topic requests to the azure.ai.agents extension's hidden
// `azd ai agent docs` command. The actual markdown is owned by (and lives
// inside) azure.ai.agents; this command is a thin forwarder.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAgentCommand returns `azd ai doc agent`. When invoked with no
// positional arg, prints the topic list. When invoked with a positional
// topic name, prints that topic body.
//
// This is a single entry point an agent uses to load just
// the slice of docs it needs.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [topic]",
		Short: "Print agent-friendly documentation for the azure.ai.agents extension.",
		Long: `Print agent-friendly documentation for the azure.ai.agents extension.

When run with no topic, lists the available topics. When run with a topic
name, prints that topic's markdown.

The markdown is owned by the azure.ai.agents extension; this command is a
thin forwarder over the hidden ` + "`azd ai agent docs --topic <name>`" + `
command in that extension. If azure.ai.agents is not installed, the error
includes the exact install command to run.`,
		Example: `  # List topics
  azd ai doc agent

  # Print one topic's markdown
  azd ai doc agent initialize
  azd ai doc agent configure
  azd ai doc agent investigate
  azd ai doc agent operate`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := ""
			if len(args) > 0 {
				topic = args[0]
			}

			// agentSiblings[0] is the agents entry. Kept as a slice in
			// doc_index.go so future siblings (toolboxes, projects, etc.)
			// can be added without restructuring.
			sibling := agentSiblings[0]

			body, err := runSiblingDocs(cmd.Context(), sibling, topic)
			if err != nil {
				return err
			}

			// Forward verbatim. Trim a single trailing newline so chained
			// `azd ai doc agent <topic> | <something>` pipelines don't get
			// a double blank line.
			body = strings.TrimRight(body, "\n") + "\n"

			if _, err := fmt.Fprint(cmd.OutOrStdout(), body); err != nil {
				return err
			}
			return nil
		},
	}

	return cmd
}
