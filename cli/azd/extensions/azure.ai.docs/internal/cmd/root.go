// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"azure.ai.docs/internal/helpformat"

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
and the leaf prints a single topic.`,
	})

	// Examples migrated into a styled Examples block (auto-promoted by
	// helpformat.InstallAll below from cmd.Example). The inline
	// Examples: section previously embedded in Long is removed so the
	// help output has exactly one Examples section, not two.
	rootCmd.Example = `  # List ai.* extensions with docs
  azd ai doc

  # List topics for the agents extension
  azd ai doc agent

  # Print one topic's markdown
  azd ai doc agent initialize`

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
	rootCmd.AddCommand(newSkillsCommand(extCtx))
	rootCmd.AddCommand(newVersionCommand(&extCtx.OutputFormat))
	rootCmd.AddCommand(newMetadataCommand(rootCmd))

	// docs root has no custom HelpFunc (unlike the agents root which
	// brackets UsageString with a banner + env-vars + skills sections),
	// so it can take the full Install treatment: HelpTemplate plus
	// UsageTemplate, plus auto-migration of cmd.Example into a styled
	// Examples block. Install runs FIRST so the root's HelpTemplate is
	// set; InstallAll then walks subcommands (agent, skills+install,
	// version, metadata) and skips the already-installed root.
	helpformat.Install(rootCmd, helpformat.Options{})
	helpformat.InstallAll(rootCmd)

	return rootCmd
}
