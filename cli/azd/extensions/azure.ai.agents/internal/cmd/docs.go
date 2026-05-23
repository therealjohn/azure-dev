// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// docs.go implements `azd ai agent docs` -- a hidden command that returns
// embedded skill markdown so the upcoming `azure.ai.docs` extension (Phase 3)
// can route topic requests through this command rather than re-distributing
// the same content.
//
// Today the command is hidden: humans should reach for `--help` or the
// online docs, not `azd ai agent docs`. Once `azd ai doc agent <topic>` lands
// in the docs extension, it shells out to this command to render each topic.

package cmd

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

// skillsFS embeds the four canonical topic markdowns. Add a new topic by
// dropping a .md file into internal/docs/skills/ -- the listTopics helper
// will pick it up automatically.
//
//go:embed docs/skills/*.md
var skillsFS embed.FS

const skillsDir = "docs/skills"

// Output format values accepted by `azd ai agent docs --output`. Distinct
// from the canonical OutputFormatTable/OutputFormatJSON used by data-returning
// leaves because the docs default is "md" (the topic body verbatim), not a
// human table. Wire-stable: change with care.
const (
	docsOutputMarkdown = "md"
	docsOutputJSON     = "json"
)

// docsFlags carries the Cobra-bound flag values for `azd ai agent docs`.
type docsFlags struct {
	topic  string
	output string
}

func newDocsCommand(extCtx *azdext.ExtensionContext) *cobra.Command {
	extCtx = ensureExtensionContext(extCtx)
	flags := &docsFlags{}

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Return embedded skill markdown for the agents extension. (hidden)",
		Long: `Return embedded skill markdown for the agents extension.

The agents extension ships four agent-friendly topic documents:

  initialize    Bootstrap a Foundry agent project end-to-end.
  configure     Shape the agent before deploying.
  investigate   Inspect agent state, sessions, evals, and optimizations.
  operate       Run write commands, billed jobs, and destructive ops.

Pass --topic to print a single topic. Pass --output md (default) for the
raw markdown or --output json for an envelope with topic + body.

This command is intentionally hidden: the typical entry point for agents
is the future ` + "`azd ai doc agent <topic>`" + ` command in the
azure.ai.docs extension, which shells out to this command to render each
topic. Humans should reach for --help or the online docs.`,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags.output = extCtx.OutputFormat
			if err := validateDocsOutputFormat(flags.output); err != nil {
				return err
			}
			return runDocs(flags)
		},
	}

	cmd.Flags().StringVar(&flags.topic, "topic", "",
		"Topic to print. Omit to list available topics. "+
			"Valid: initialize, configure, investigate, operate.")

	// --output is the azd reserved global; we register the docs-specific
	// allowed values (md/json) via the SDK hook so the host accepts the
	// flag and substitutes the default before RunE rather than rejecting
	// the command as defining a conflicting flag.
	azdext.RegisterFlagOptions(cmd, azdext.FlagOptions{
		Name:          "output",
		AllowedValues: []string{docsOutputMarkdown, docsOutputJSON},
		Default:       docsOutputMarkdown,
	})

	return cmd
}

// docsTopicResponse is the JSON shape for `azd ai agent docs --output json`.
// Stable contract -- additive changes only. Body is markdown so the docs
// extension (Phase 3) can re-render it however it likes.
type docsTopicResponse struct {
	Topic string `json:"topic"`
	Body  string `json:"body"`
}

// docsListResponse is the JSON shape for `azd ai agent docs --output json`
// when no --topic is set. Stable contract -- additive changes only.
type docsListResponse struct {
	Topics []string `json:"topics"`
}

// validateDocsOutputFormat rejects values other than md/json. The SDK
// pre-parse sentinel "default" and empty string both resolve to md (the
// docs-command default registered with RegisterFlagOptions).
func validateDocsOutputFormat(out string) error {
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "", "default", docsOutputMarkdown, docsOutputJSON:
		return nil
	default:
		return exterrors.Validation(
			exterrors.CodeInvalidParameter,
			fmt.Sprintf("invalid --output value %q", out),
			"use md or json",
		)
	}
}

// isDocsJSON reports whether the resolved --output value selects the JSON
// envelope shape. Empty / "default" / "md" all return false.
func isDocsJSON(out string) bool {
	return strings.EqualFold(strings.TrimSpace(out), docsOutputJSON)
}

func runDocs(flags *docsFlags) error {
	if flags.topic == "" {
		return listTopics(os.Stdout, flags.output)
	}
	return printTopic(os.Stdout, flags.topic, flags.output)
}

// listTopics enumerates the embedded topics in alphabetical order. Returned
// names match the filename stems under docs/skills/ (no extension).
//
// Accepts a writer so tests can drive listTopics without going through the
// os.Stdout pipe-swap dance, which deadlocks on Windows when a single write
// exceeds the default ~4KB anonymous-pipe buffer.
func listTopics(w io.Writer, output string) error {
	entries, err := fs.ReadDir(skillsFS, skillsDir)
	if err != nil {
		return exterrors.Internal(
			"skills_dir_read_failed",
			fmt.Sprintf("read embedded skills dir: %s", err),
		)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if name == e.Name() {
			// Non-markdown file co-located by mistake; skip.
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	if isDocsJSON(output) {
		return printJSON(w, docsListResponse{Topics: names})
	}

	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return nil
}

// printTopic emits the body of a single topic. Treats unknown topics as a
// validation error with a structured suggestion listing what IS available.
//
// Same writer parameter rationale as listTopics: avoid the Windows pipe
// deadlock that bites tests using captureStdout for topics > 4KB.
func printTopic(w io.Writer, topic, output string) error {
	body, err := fs.ReadFile(skillsFS, fmt.Sprintf("%s/%s.md", skillsDir, topic))
	if err != nil {
		// Build a friendly suggestion that lists what topics DO exist.
		entries, _ := fs.ReadDir(skillsFS, skillsDir)
		var known []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			known = append(known, strings.TrimSuffix(e.Name(), ".md"))
		}
		sort.Strings(known)
		return exterrors.Validation(
			exterrors.CodeInvalidParameter,
			fmt.Sprintf("unknown topic %q", topic),
			fmt.Sprintf("valid topics: %s", strings.Join(known, ", ")),
		)
	}

	if isDocsJSON(output) {
		return printJSON(w, docsTopicResponse{Topic: topic, Body: string(body)})
	}

	if _, err := w.Write(body); err != nil {
		return err
	}
	// Ensure trailing newline so terminal users get a clean prompt back.
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte{'\n'})
	}
	return nil
}
