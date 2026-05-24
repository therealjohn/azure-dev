// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"fmt"

	conncmd "azureaiagent/internal/connections/cmd"
	"azureaiagent/internal/helpformat"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	rootCmd, extCtx := azdext.NewExtensionRootCommand(azdext.ExtensionCommandOptions{
		Name:  "agent",
		Use:   "agent <command> [options]",
		Short: fmt.Sprintf("Ship agents with Microsoft Foundry from your terminal. %s", color.YellowString("(Preview)")),
	})
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Configure debug logging once on the root command so every subcommand
	// inherits it (cobra.EnableTraverseRunHooks, set by the SDK, ensures this
	// runs alongside any subcommand pre-runs). The cleanup func is intentionally
	// discarded: log writes are unbuffered and the OS closes the file on exit.
	sdkPreRun := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if sdkPreRun != nil {
			if err := sdkPreRun(cmd, args); err != nil {
				return err
			}
		}
		setupDebugLogging(cmd.Flags())
		return nil
	}

	// Show the ASCII art banner + state-aware "Get started" preamble +
	// Environments & Environment Variables + Docs & Agent Skills on `azd ai agent --help`.
	// installAgentsHelpOutput wraps the default cobra help func so subcommand
	// --help output is unaffected.
	installAgentsHelpOutput(rootCmd)

	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	rootCmd.AddCommand(azdext.NewListenCommand(configureExtensionHost))
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newInitCommand(extCtx))
	rootCmd.AddCommand(newRunCommand(extCtx))
	rootCmd.AddCommand(newInvokeCommand(extCtx))
	rootCmd.AddCommand(newMcpCommand())
	rootCmd.AddCommand(azdext.NewMetadataCommand("1.0", "azure.ai.agents", func() *cobra.Command {
		return rootCmd
	}))
	rootCmd.AddCommand(newShowCommand(extCtx))
	rootCmd.AddCommand(newEndpointCommand(extCtx))
	rootCmd.AddCommand(newMonitorCommand(extCtx))
	rootCmd.AddCommand(newFilesCommand(extCtx))
	rootCmd.AddCommand(newSessionCommand(extCtx))
	rootCmd.AddCommand(newSampleCommand(extCtx))
	rootCmd.AddCommand(newDoctorCommand(extCtx))

	// Connection commands — in separate package for easy lift-and-shift later.
	// When the azd core namespace change lands, move this AddCommand call
	// to the new root and update the import path.
	rootCmd.AddCommand(conncmd.NewConnectionRootCommand(extCtx))
	rootCmd.AddCommand(newEvalCommand(extCtx))
	rootCmd.AddCommand(newOptimizeCommand(extCtx))

	// Apply styled --help to every visible command in the tree. Commands
	// that opted into custom Description/Footer (e.g. init) already
	// called helpformat.Install and are skipped by InstallAll. The root
	// command itself gets InstallUsageOnly so installAgentsHelpOutput's
	// bespoke HelpFunc continues to drive the banner + state-aware
	// preamble + env vars + docs sections.
	helpformat.InstallAll(rootCmd)

	return rootCmd
}
