// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// doc_index.go implements `azd ai doc` (the top-level index). It lists
// which sibling ai.* extensions are reachable for documentation and what
// each one ships.
//
// Today the index only knows about `azure.ai.agents` because that is the
// only sibling that exposes a docs integration command. As other ai.*
// extensions adopt the pattern, register them in agentSiblings below.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// docSibling describes one ai.* extension's docs integration. CommandPath
// is the *array* of args to pass to `azd` to invoke the sibling's hidden
// `docs` command. Stable -- agents and humans both rely on the words in
// SubcommandName ("agent", "project", etc.) staying constant.
type docSibling struct {
	// SubcommandName is the leaf verb under `azd ai doc` (e.g. "agent").
	SubcommandName string
	// DisplayName is shown in `azd ai doc`'s top-level table.
	DisplayName string
	// CommandPath is the argv list the index command shells out to in order
	// to fetch the sibling's topic list / topic body. The index passes
	// "--topic <name>" or omits it as needed.
	CommandPath []string
}

// agentSiblings registers every sibling extension this docs front door
// knows about. Add a new entry when another ai.* extension ships its own
// `docs` integration command.
var agentSiblings = []docSibling{
	{
		SubcommandName: "agent",
		DisplayName:    "Foundry agents (azure.ai.agents)",
		CommandPath:    []string{"ai", "agent", "docs"},
	},
}

// runDocIndex is the RunE for the bare `azd ai doc` command. Renders a
// short table of available sibling extensions. Returns nil so the process
// exits 0 even when no siblings are registered yet.
func runDocIndex(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Available documentation:")
	fmt.Fprintln(out)
	for _, s := range agentSiblings {
		fmt.Fprintf(out, "  %-30s azd ai doc %s\n", s.DisplayName, s.SubcommandName)
	}
	return nil
}

// runSiblingDocs shells out to a sibling extension's hidden `docs` command.
// When topic is empty, asks the sibling for the topic list (one name per
// line). When topic is set, asks the sibling for the topic body.
//
// Errors when the sibling is not installed are translated into a friendly
// "install this extension" hint so agents and humans know exactly what to
// run next, rather than seeing a generic exec.ExitError.
func runSiblingDocs(ctx context.Context, sibling docSibling, topic string) (string, error) {
	args := append([]string(nil), sibling.CommandPath...)
	if topic != "" {
		args = append(args, "--topic", topic)
	}

	// Generous timeout: most topic bodies are < 10 KB and load from an
	// embedded FS, but the sibling extension still has to boot the azd
	// host shim. 30s is comfortable headroom on a cold start.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "azd", args...) //nolint:gosec // args are constants
	cmd.Stderr = os.Stderr                          // surface azd host errors to the user verbatim
	output, err := cmd.Output()
	if err != nil {
		// Distinguish "azd binary missing" from "command failed".
		var pathErr *exec.Error
		if errors.As(err, &pathErr) {
			return "", fmt.Errorf(
				"could not invoke azd: %w\n\n"+
					"This command requires the azd CLI to be installed and on PATH.",
				err)
		}
		// Distinguish "sibling extension missing" by recognizing the
		// "unknown command" exit shape from cobra (exit code 1 with the
		// sibling subcommand name on stderr). Any other failure shape
		// surfaces as an internal error so the user sees what happened.
		return "", fmt.Errorf(
			"%s failed: %w\n\n"+
				"Install or update the %s extension:\n"+
				"    azd ext install %s",
			strings.Join(args, " "),
			err,
			sibling.DisplayName,
			extensionIDForSibling(sibling),
		)
	}

	return string(output), nil
}

// extensionIDForSibling maps a docSibling's SubcommandName back to the
// extension ID a user would pass to `azd ext install`. Kept inline because
// the mapping is small and won't change often.
func extensionIDForSibling(s docSibling) string {
	switch s.SubcommandName {
	case "agent":
		return "azure.ai.agents"
	default:
		return s.SubcommandName
	}
}
