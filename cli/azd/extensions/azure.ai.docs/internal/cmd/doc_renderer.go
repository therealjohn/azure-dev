// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// doc_renderer.go produces the styled body and Examples sections for
// `azd ai doc` and `azd ai doc <category>`. The two outputs are split
// so callers can wire them into helpformat.Install's Description and
// Footer slots separately, avoiding the double-Examples render that
// happens when both Description and cmd.Example are set.
//
// Direct invocations (RunE) concatenate Body + Examples to produce the
// same content `--help` shows above its Usage / Flags blocks.

package cmd

import (
	"fmt"
	"strings"

	"azure.ai.docs/internal/helpformat"
)

// renderRootBody returns the rendered body for `azd ai doc`: preamble
// followed by an "Available Documentation:" section listing every
// category with its Short description.
//
// The section is named "Available Documentation" (NOT "Available
// Commands") because the docs extension's root cobra command has REAL
// subcommands (agent, skills, version, metadata) that cobra renders
// under its own "Available Commands:" header from the styled
// UsageTemplate. Two sections with the same name in one help output
// would confuse a reader; the rename makes the catalog intent explicit.
func renderRootBody(cats []DocCategory) string {
	var b strings.Builder
	notes := []string{
		helpformat.Note(fmt.Sprintf(
			"Each command group below collects workflow docs an AI coding assistant can "+
				"read directly to drive the matching %s write commands.",
			helpformat.Command("azd ai *"),
		)),
		helpformat.Note("Topic bodies are self-contained markdown -- pipe to a model or print to a terminal."),
	}
	b.WriteString(helpformat.Description(
		"The agent-friendly documentation front door for Azure AI Foundry extensions.",
		notes...,
	))
	b.WriteString(helpformat.SectionHeader("Available Documentation"))
	b.WriteString("\n")
	width := categoryColumnWidth(cats)
	for _, c := range cats {
		b.WriteString("  ")
		b.WriteString(helpformat.Command(c.Name))
		b.WriteString(padRight(c.Name, width))
		b.WriteString(": ")
		b.WriteString(c.Short)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// renderRootExamples returns just the styled Examples block for the
// root catalog. Aggregates a couple of representative examples that
// help an agent discover where to go next. Returned separately from
// renderRootBody so it can flow through helpformat.Options.Footer.
func renderRootExamples(cats []DocCategory) string {
	samples := map[string]string{
		// Lexical sort: "List ..." (L) < "Print ..." (P). Titles
		// chosen so the sorted order reads as a natural progression.
		"List available documentation groups.": "azd ai doc",
	}
	if len(cats) > 0 {
		samples[fmt.Sprintf("List topics for the %s group.", cats[0].Name)] = fmt.Sprintf(
			"azd ai doc %s", cats[0].Name,
		)
		if len(cats[0].Topics) > 0 {
			samples[fmt.Sprintf("Print the %s topic body.", cats[0].Topics[0].Name)] = fmt.Sprintf(
				"azd ai doc %s %s", cats[0].Name, cats[0].Topics[0].Name,
			)
		}
	}
	return helpformat.Examples(samples)
}

// renderCatalogBody returns the rendered body for `azd ai doc <category>`:
// preamble followed by "Available Commands:" (topics + descriptions)
// followed by optional "References for `<topic>`:" blocks for any topic
// whose References field is non-empty.
//
// "Available Commands:" is safe to use at the category level because
// topics are positional args -- there is no cobra-side Available
// Commands section to conflict with on the agent command.
func renderCatalogBody(cat DocCategory) string {
	var b strings.Builder
	title := fmt.Sprintf("Agent-friendly workflow documentation for the %s extension.",
		categoryExtensionName(cat))
	notes := make([]string, 0, len(cat.Preamble))
	for _, p := range cat.Preamble {
		notes = append(notes, helpformat.Note(p))
	}
	b.WriteString(helpformat.Description(title, notes...))
	b.WriteString(helpformat.SectionHeader("Available Commands"))
	b.WriteString("\n")
	width := topicColumnWidth(cat.Topics)
	for _, t := range cat.Topics {
		b.WriteString("  ")
		b.WriteString(helpformat.Command(t.Name))
		b.WriteString(padRight(t.Name, width))
		b.WriteString(": ")
		b.WriteString(t.Short)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	// One References block per topic that has any. Topics with no
	// references are skipped entirely so a category whose topics all
	// lack references produces no References output.
	for _, t := range cat.Topics {
		if len(t.References) == 0 {
			continue
		}
		b.WriteString(helpformat.SectionHeader(fmt.Sprintf("References for `%s`", t.Name)))
		b.WriteString("\n")
		refWidth := referenceColumnWidth(t.References)
		for _, r := range t.References {
			b.WriteString("  ")
			b.WriteString(helpformat.Command(r.Name))
			b.WriteString(padRight(r.Name, refWidth))
			b.WriteString(": ")
			b.WriteString(r.Short)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderCatalogExamples returns just the styled Examples block for one
// category. Reads cat.Examples directly so the catalog owns the per-
// category guidance.
func renderCatalogExamples(cat DocCategory) string {
	return helpformat.Examples(cat.Examples)
}

// padRight returns the space padding needed to right-align the colon
// after a name column. Visible-width based -- ANSI escapes around the
// styled name are zero-width on terminals so the visible column still
// aligns with this trivial computation.
func padRight(name string, width int) string {
	if len(name) >= width {
		return ""
	}
	return strings.Repeat(" ", width-len(name))
}

// categoryColumnWidth returns the longest category name across cats,
// used as the right-pad target for the Available Documentation list.
func categoryColumnWidth(cats []DocCategory) int {
	w := 0
	for _, c := range cats {
		if len(c.Name) > w {
			w = len(c.Name)
		}
	}
	return w
}

// topicColumnWidth returns the longest topic name across topics, used
// as the right-pad target for the Available Commands list.
func topicColumnWidth(topics []DocTopic) int {
	w := 0
	for _, t := range topics {
		if len(t.Name) > w {
			w = len(t.Name)
		}
	}
	return w
}

// referenceColumnWidth is the per-topic equivalent of topicColumnWidth,
// scoped to one References block so each block aligns independently.
func referenceColumnWidth(refs []DocReference) int {
	w := 0
	for _, r := range refs {
		if len(r.Name) > w {
			w = len(r.Name)
		}
	}
	return w
}

// categoryExtensionName maps a category Name to its full ai.*
// extension identifier used in the preamble sentence. Today only
// `agent` ships; future categories add a case here. Falls back to a
// generic phrasing so a new category that forgets to update this map
// still produces a sensible preamble.
func categoryExtensionName(cat DocCategory) string {
	switch cat.Name {
	case "agent":
		return "azure.ai.agents"
	default:
		return fmt.Sprintf("azure.ai.%s", cat.Name)
	}
}
