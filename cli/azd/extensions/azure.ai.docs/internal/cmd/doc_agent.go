// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// doc_agent.go implements `azd ai doc agent [topic]` -- prints embedded
// agent-friendly markdown from skills/agent/*.md. The markdown is owned
// by (and lives in) this extension; each topic is a self-contained
// contract the agent reads to drive `azd ai agent` write commands.
//
// Per-extension topic folders live at skills/<sibling>/. As other ai.*
// extensions get their own topic sets, add a sibling subdir and a
// matching subcommand here (newToolboxCommand, newProjectCommand, etc.).

package cmd

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// skillsFS embeds every topic markdown shipped by this extension. Add a
// new sibling-extension topic group by creating skills/<sibling>/<topic>.md
// files; the listTopics helper picks them up automatically.
//
//go:embed skills/*/*.md
var skillsFS embed.FS

const skillsRoot = "skills"

// newAgentCommand returns `azd ai doc agent [topic]`. When invoked with
// no positional arg, prints the agent-extension topic list. When invoked
// with a positional topic name, prints that topic body.
//
// Acts as a single entry point an agent uses to load just the slice of
// docs it needs to drive the matching CLI commands.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [topic]",
		Short: "Print agent-friendly documentation for the azure.ai.agents extension.",
		Long: `Print agent-friendly documentation for the azure.ai.agents extension.

When run with no topic, lists available topic names. When run with a
topic name, prints that topic's markdown.

Topics shipped today:

  initialize    Bootstrap a Foundry agent project end-to-end.
  configure     Shape the agent before deploying.
  investigate   Inspect agent state, sessions, evals, and optimizations.
  operate       Run write commands, billed jobs, and destructive ops.`,
		Example: `  # List topics
  azd ai doc agent

  # Print one topic's markdown
  azd ai doc agent initialize
  azd ai doc agent configure
  azd ai doc agent investigate
  azd ai doc agent operate`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listCategoryTopics(cmd.OutOrStdout(), "agent")
			}
			return printCategoryTopic(cmd.OutOrStdout(), "agent", args[0])
		},
	}
	return cmd
}

// listCategoryTopics enumerates the topics under skills/<category>/ in
// alphabetical order. Names match the filename stems with no extension.
//
// Accepts a writer parameter so tests can drive it with a bytes.Buffer
// without going through the captureStdout pipe-swap helper, which
// deadlocks on Windows for writes larger than the ~4KB anonymous-pipe
// buffer (and topic bodies routinely exceed that).
func listCategoryTopics(w io.Writer, category string) error {
	dir := categoryDir(category)
	entries, err := fs.ReadDir(skillsFS, dir)
	if err != nil {
		return fmt.Errorf("read embedded skills dir %q: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		if stem == e.Name() {
			continue // non-markdown file, skip
		}
		names = append(names, stem)
	}
	sort.Strings(names)

	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return nil
}

// printCategoryTopic prints the markdown body for one topic. Unknown
// topics return an error that lists the valid topics, so an agent that
// mistypes a topic can self-correct without a doc lookup.
func printCategoryTopic(w io.Writer, category, topic string) error {
	path := fmt.Sprintf("%s/%s.md", categoryDir(category), topic)
	body, err := fs.ReadFile(skillsFS, path)
	if err != nil {
		known, _ := readCategoryTopicNames(category)
		return fmt.Errorf(
			"unknown topic %q. Valid topics: %s",
			topic, strings.Join(known, ", "))
	}

	if _, err := w.Write(body); err != nil {
		return err
	}
	// Trailing newline so terminal users get a clean prompt back.
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte{'\n'})
	}
	return nil
}

// categoryDir returns the embedded-FS directory for a sibling-extension
// topic group. Centralized so a future tweak to the layout only changes
// one line.
func categoryDir(category string) string {
	return skillsRoot + "/" + category
}

// readCategoryTopicNames returns the sorted topic names for a category.
// Used by printCategoryTopic to render a helpful "did you mean" list when
// a topic name is unknown.
func readCategoryTopicNames(category string) ([]string, error) {
	entries, err := fs.ReadDir(skillsFS, categoryDir(category))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}
