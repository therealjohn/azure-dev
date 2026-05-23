// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	rootCmd, extCtx := azdext.NewExtensionRootCommand(azdext.ExtensionCommandOptions{
		Name:  "doc",
		Use:   "doc <command> [options]",
		Short: "Agent-ready documentation for the Foundry azd extensions. (Preview)",
		Long: `Single front door for agent-friendly documentation across every
azure.ai.* extension.

Each sibling ai.* extension owns its own embedded markdown topics; this
extension routes topic requests to the right extension and renders the
result. The shape mirrors a familiar "skills" surface: top-level lists the
covered ai.* extensions, the next level lists topics for an extension,
and the leaf prints a single topic.

Examples:

  # List ai.* extensions with docs
  azd ai doc

  # List topics for the agents extension
  azd ai doc agent

  # Print one topic's markdown
  azd ai doc agent initialize`,
	})

	// The root command itself renders the top-level index when invoked
	// with no subcommand. Matches a familiar `skills` catalog shape so
	// agents can discover available docs without first knowing a verb.
	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = runDocIndex

	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.CompletionOptions = cobra.CompletionOptions{
		DisableDefaultCmd: true,
	}

	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	rootCmd.AddCommand(newAgentCommand())
	rootCmd.AddCommand(newVersionCommand(&extCtx.OutputFormat))
	rootCmd.AddCommand(newMetadataCommand(rootCmd))

	return rootCmd
}
