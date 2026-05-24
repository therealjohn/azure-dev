// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// Package helpformat renders styled `--help` output for cobra commands.
// It mirrors the visual rhythm of `azd init --help` (underlined section
// headers, bulleted preamble, colored Examples) for extension commands.
//
// TODO: candidate for promotion to cli/azd/pkg/azdext/cmdhelp/ as a
// shared SDK package once a third extension needs the same styling.
// Until then, keep this file LITERALLY in sync with its mirror in the
// other azd-ai-* extension:
//
//	cli/azd/extensions/azure.ai.agents/internal/helpformat/helpformat.go
//
// Design notes:
//
//   - Install sets cmd.SetUsageTemplate + cmd.SetHelpTemplate. We deliberately
//     do NOT use cmd.SetHelpFunc -- the SDK's NewExtensionRootCommand wraps
//     cmd.UsageFunc to apply per-command flag-option overrides registered
//     via azdext.RegisterFlagOptions. The HelpTemplate body uses cobra's
//     `{{.UsageString}}` directive which routes through that wrapper, so
//     flag overrides keep rendering correctly. SetHelpFunc would bypass it.
//
//   - Dynamic sections (Available Commands, Flags, Global Flags) render via
//     cobra template funcs registered once via sync.Once. Reading live
//     command state at render time means inherited persistent flags and
//     late-added subcommands are picked up automatically.
//
//   - Static slots (Description and Footer) are pre-rendered at Install
//     time. They typically come from helpformat.Description / .Examples
//     builders defined in this package.
//
//   - Colors are applied via pkg/output, which delegates to fatih/color.
//     fatih/color evaluates color.NoColor at Sprint time, not at install
//     time. Help text rendered at help-call time therefore honors the
//     ambient NO_COLOR / color.NoColor setting at that moment. Tests can
//     toggle color per-test with t.Cleanup.
package helpformat

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Options controls the description and footer slots of the styled help.
// The dynamic sections (Usage line, Aliases, Available Commands, Flags,
// Global Flags) come from cobra template funcs and do not need to be
// supplied here.
type Options struct {
	// Description renders the help block above the Usage section.
	// Typically built with Description(title, notes...). When nil, the
	// command's cobra.Command.Long (or Short if Long is empty) is used.
	Description func(cmd *cobra.Command) string

	// Footer renders the help block below the Global Flags section.
	// Typically built with Examples(samples). Nil means no footer.
	Footer func(cmd *cobra.Command) string
}

// Install wires the styled help template onto cmd. Safe to call multiple
// times; the last call wins. Idempotent w.r.t. template-func registration.
//
// Call Install AFTER every cmd.AddCommand(...) for this command. The
// template funcs read live state at render time, so a late AddCommand
// will still appear in Available Commands, but the call-site convention
// helps reviewers reason about the final command tree.
func Install(cmd *cobra.Command, opts Options) {
	registerTemplateFuncs()
	cmd.SetUsageTemplate(styledUsageTemplate)
	cmd.SetHelpTemplate(buildHelpTemplate(cmd, opts))
}

// InstallUsageOnly wires only the styled UsageTemplate onto cmd, leaving
// the HelpTemplate (and any SetHelpFunc) untouched. This exists for the
// agents root command, whose bespoke HelpFunc continues to call
// cmd.UsageString() between a banner and trailing sections; installing a
// HelpTemplate would have no effect (the HelpFunc takes precedence) but
// using a dedicated entry point makes intent explicit at the call site.
func InstallUsageOnly(cmd *cobra.Command) {
	registerTemplateFuncs()
	cmd.SetUsageTemplate(styledUsageTemplate)
}

// Description renders a preamble: a one-line title followed by bulleted
// notes. Returns "title\n\n" when notes is empty. Notes should already
// be wrapped by Note() for the bullet glyph.
func Description(title string, notes ...string) string {
	if len(notes) == 0 {
		return title + "\n\n"
	}
	return fmt.Sprintf("%s\n\n%s\n\n", title, strings.Join(notes, "\n"))
}

// Note wraps a single bullet line with "  * <text>". ASCII bullet because
// this codebase requires ASCII output (per repo style rules).
func Note(text string) string {
	return "  * " + text
}

// Examples renders an underlined "Examples" header followed by
// "title\n    command" pairs, sorted deterministically by title.
// Returns "" when samples is empty.
func Examples(samples map[string]string) string {
	if len(samples) == 0 {
		return ""
	}
	lines := make([]string, 0, len(samples))
	for title, command := range samples {
		lines = append(lines, fmt.Sprintf("  %s\n    %s", title, command))
	}
	slices.Sort(lines)
	return fmt.Sprintf("%s\n%s\n", sectionHeader("Examples"), strings.Join(lines, "\n\n"))
}

// Flag renders a flag token in blue (e.g. "--template" inside a bullet).
func Flag(s string) string { return output.WithHighLightFormat("%s", s) }

// Command renders a command token in blue (e.g. "azd init" inside a bullet).
// Kept distinct from Flag so call sites read clearly; both currently render
// the same blue but the names let us diverge later without touching callers.
func Command(s string) string { return output.WithHighLightFormat("%s", s) }

// Arg renders an argument placeholder in yellow (e.g. "[GitHub repo URL]"
// inside an example). Matches the convention from azd init --help.
func Arg(s string) string { return output.WithWarningFormat("%s", s) }

// Link renders a URL in the hyperlink-looking cyan, matching core azd.
func Link(s string) string { return output.WithLinkFormat("%s", s) }

// --- Template machinery (private) --------------------------------------------

// nonPersistentGlobalFlags duplicates cli/azd/internal/cmd.NonPersistentGlobalFlags.
// That package is not importable across module boundaries, so we mirror it
// here. If azd ever adds another forced-global (e.g. --quiet) update here.
// Forced-globals are only rendered in Global Flags when they actually exist
// as local flags on the command (so e.g. --docs stays hidden until the SDK
// surfaces it on extension commands).
var nonPersistentGlobalFlags = []string{"help", "docs"}

// endOfTitleSentinel matches core azd's alignment trick. A NUL byte cannot
// appear in flag names or types, so it's a safe in-band marker for the
// "split between flag title and description" column.
const endOfTitleSentinel = "\x00"

var (
	templateFuncsOnce sync.Once
	// styledUsageTemplate is the cobra template body for Usage / Aliases /
	// Available Commands / Flags / Global Flags. Built once at package init
	// time by buildStyledUsageTemplate. Pre-rendered ANSI escapes for the
	// section headers are baked into the literal because the headers are
	// constant strings; the dynamic bodies are rendered at help time via
	// the registered template funcs.
	styledUsageTemplate = buildStyledUsageTemplate()
)

// registerTemplateFuncs adds our helper funcs to cobra's template registry.
// cobra.AddTemplateFunc is process-global state; sync.Once prevents double
// registration when Install is called many times across a single process.
// The funcs themselves are read-only over cmd state, so concurrent help
// rendering (which cobra serializes anyway) is safe.
func registerTemplateFuncs() {
	templateFuncsOnce.Do(func() {
		cobra.AddTemplateFunc("helpformatLocalFlags", helpformatLocalFlags)
		cobra.AddTemplateFunc("helpformatGlobalFlags", helpformatGlobalFlags)
		cobra.AddTemplateFunc("helpformatHasGlobalFlags", helpformatHasGlobalFlags)
		cobra.AddTemplateFunc("helpformatCommands", helpformatCommands)
		cobra.AddTemplateFunc("helpformatHasCommands", helpformatHasCommands)
	})
}

// buildStyledUsageTemplate composes the styled UsageTemplate string.
// Mirrors cobra's default UsageTemplate shape with these changes:
//   - Section headers run through sectionHeader (bold + underline).
//   - Available Commands and Flags bodies use our template funcs so we
//     control alignment and (in the case of Flags) the forced-globals split.
//   - The Examples section is OMITTED here -- our migrated examples live
//     in the HelpTemplate footer via the Examples builder, not in
//     cmd.Example. Leaving the default Examples directive in this template
//     would double-render when a caller forgets to clear cmd.Example.
//
// The Usage line follows core azd's exact conditional pattern so verbose
// `Use:` strings on parent commands (e.g. `agent <command> [options]`)
// do not produce a duplicated `[command]` suffix.
func buildStyledUsageTemplate() string {
	// Build the template as a multi-line string. Each section is wrapped
	// in its own {{if ...}}...{{end}} so empty sections produce no output
	// (no stray blank lines).
	var b strings.Builder

	// Usage section: always rendered.
	b.WriteString(sectionHeader("Usage"))
	b.WriteString("\n  {{if .Runnable}}{{.UseLine}}{{end}}")
	b.WriteString("{{if .HasAvailableSubCommands}}{{.CommandPath}} [command]{{end}}\n")

	// Aliases section: only when set.
	b.WriteString("{{if gt (len .Aliases) 0}}\n")
	b.WriteString(sectionHeader("Aliases"))
	b.WriteString("\n  {{.NameAndAliases}}\n{{end}}")

	// Available Commands section.
	b.WriteString("{{if helpformatHasCommands .}}\n")
	b.WriteString(sectionHeader("Available Commands"))
	b.WriteString("\n{{helpformatCommands .}}\n{{end}}")

	// Local Flags section.
	b.WriteString("{{if .HasAvailableLocalFlags}}\n")
	b.WriteString(sectionHeader("Flags"))
	b.WriteString("\n{{helpformatLocalFlags .}}\n{{end}}")

	// Global Flags section -- uses our helper (not HasAvailableInheritedFlags)
	// so forced-globals (--help, --docs when registered) are included.
	b.WriteString("{{if helpformatHasGlobalFlags .}}\n")
	b.WriteString(sectionHeader("Global Flags"))
	b.WriteString("\n{{helpformatGlobalFlags .}}\n{{end}}")

	return b.String()
}

// buildHelpTemplate composes the HelpTemplate for a specific cmd + opts.
// Layout: <description>{{.UsageString}}<footer>.
//
//   - <description> is opts.Description(cmd) when provided, else the
//     command's Long (or Short) text with a trailing blank line. Embedded
//     as a literal string -- cobra does not interpret template directives
//     inside the description text we inject.
//   - {{.UsageString}} routes through cmd.UsageFunc, which the SDK wraps
//     to apply per-command flag-option overrides BEFORE rendering the
//     UsageTemplate above. This is THE reason we use SetUsageTemplate +
//     SetHelpTemplate instead of SetHelpFunc.
//   - <footer> is opts.Footer(cmd) when provided, else empty.
func buildHelpTemplate(cmd *cobra.Command, opts Options) string {
	var b strings.Builder

	if opts.Description != nil {
		desc := opts.Description(cmd)
		// Ensure exactly one blank line between description and the
		// following Usage section, regardless of how the builder
		// terminated. Description() already appends "\n\n", but
		// custom callers may not, so normalize.
		desc = strings.TrimRight(desc, "\n")
		if desc != "" {
			b.WriteString(desc)
			b.WriteString("\n\n")
		}
	} else {
		// Fall back to cobra's default Long / Short precedence.
		fallback := strings.TrimRightFunc(cmd.Long, isSpace)
		if fallback == "" {
			fallback = strings.TrimRightFunc(cmd.Short, isSpace)
		}
		if fallback != "" {
			b.WriteString(fallback)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("{{.UsageString}}")

	if opts.Footer != nil {
		footer := opts.Footer(cmd)
		if footer != "" {
			// One blank line between Global Flags / Usage block and
			// the footer (typically the Examples block).
			b.WriteString("\n")
			b.WriteString(footer)
		}
	}

	return b.String()
}

func isSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\r' || r == '\t' }

// sectionHeader renders "<title>:" as bold + underlined, matching the
// header style from azd init --help.
//
// Note: The styledUsageTemplate is built ONCE at package init via the
// package-level `styledUsageTemplate = buildStyledUsageTemplate()` var
// initializer, which calls sectionHeader at that moment. The ANSI escapes
// are therefore baked in based on color.NoColor at IMPORT time. To toggle
// color in tests, set color.NoColor BEFORE the helpformat package is
// loaded (typically via TestMain). The Examples builder, in contrast, is
// called at help-render time and honors the runtime color setting.
func sectionHeader(title string) string {
	return output.WithBold("%s", output.WithUnderline("%s:", title))
}

// helpformatLocalFlags renders the Flags section body: aligned
// "  -s, --long [type]  : description" rows. Hidden flags are skipped.
// Forced-globals (--help, --docs) are EXCLUDED from this list when they
// exist as local flags, mirroring core azd's split.
func helpformatLocalFlags(cmd *cobra.Command) string {
	return renderFlagSet(localFlagsExcludingForced(cmd))
}

// helpformatGlobalFlags renders the Global Flags section body. The set
// is inherited flags plus any forced-globals that exist as LOCAL flags on
// cmd (so --help, registered automatically by cobra, lands here instead of
// the Local Flags section).
func helpformatGlobalFlags(cmd *cobra.Command) string {
	return renderFlagSet(globalFlagSetForCommand(cmd))
}

// helpformatHasGlobalFlags returns true when the Global Flags section
// would render any rows. Used by the template's {{if ...}} guard so the
// section header is suppressed for commands with no inherited or
// forced-global flags.
func helpformatHasGlobalFlags(cmd *cobra.Command) bool {
	return globalFlagSetForCommand(cmd).HasAvailableFlags()
}

// helpformatHasCommands returns true when at least one direct subcommand
// is user-visible (IsAvailableCommand). Hidden and deprecated commands
// are filtered out. Mirrors the test cobra uses internally for its own
// HasAvailableSubCommands but evaluated against our filter.
func helpformatHasCommands(cmd *cobra.Command) bool {
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			return true
		}
	}
	return false
}

// helpformatCommands renders aligned "  name  : short" rows for every
// direct subcommand. Sorted alphabetically by Use (cobra's default order).
func helpformatCommands(cmd *cobra.Command) string {
	var (
		lines []string
		width int
	)
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		name := "  " + sub.Name()
		if len(name) > width {
			width = len(name)
		}
		lines = append(lines, name+endOfTitleSentinel+sub.Short)
	}
	if width == 0 {
		return ""
	}
	alignTitles(lines, width)
	return strings.Join(lines, "\n")
}

// renderFlagSet produces the aligned "  -s, --long [type]  : description"
// body for a *pflag.FlagSet. Returns "" when the set has no visible flags.
// Lifted from cli/azd/cmd/cmd_help.go::getFlagsDetails (not importable
// across modules); kept structurally identical for visual parity.
func renderFlagSet(flags *pflag.FlagSet) string {
	var (
		lines []string
		width int
	)
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		line := ""
		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			line = fmt.Sprintf("  -%s, --%s", flag.Shorthand, flag.Name)
		} else {
			line = fmt.Sprintf("      --%s", flag.Name)
		}
		varName, usage := pflag.UnquoteUsage(flag)
		if varName != "" {
			line += " " + varName
		}
		line += endOfTitleSentinel
		if len(line) > width {
			width = len(line)
		}
		line += usage
		if flag.Deprecated != "" {
			line += fmt.Sprintf(" (DEPRECATED: %s)", flag.Deprecated)
		}
		lines = append(lines, line)
	})
	if width == 0 {
		return ""
	}
	alignTitles(lines, width)
	return "  " + strings.Join(lines, "\n  ")
}

// alignTitles right-pads the per-line title prefix (everything before the
// endOfTitleSentinel) so all lines share the same column for the ": desc"
// suffix. Mirrors cli/azd/cmd/cmd_help.go::alignTitles.
func alignTitles(lines []string, longest int) {
	for i, line := range lines {
		idx := strings.Index(line, endOfTitleSentinel)
		if idx < 0 {
			continue
		}
		pad := strings.Repeat(" ", longest-idx)
		lines[i] = fmt.Sprintf("%s%s\t: %s", line[:idx], pad, line[idx+1:])
	}
}

// localFlagsExcludingForced returns cmd.LocalFlags() with any forced-
// global flag names (e.g. "help", "docs") REMOVED. Those move to the
// Global Flags section via globalFlagSetForCommand below.
func localFlagsExcludingForced(cmd *cobra.Command) *pflag.FlagSet {
	out := pflag.NewFlagSet("", pflag.ContinueOnError)
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if slices.Contains(nonPersistentGlobalFlags, f.Name) {
			return
		}
		out.AddFlag(f)
	})
	return out
}

// globalFlagSetForCommand builds the flag set used for the Global Flags
// section: inherited flags from parents PLUS any forced-globals that
// actually exist as LOCAL flags on cmd. The Lookup guard means --docs
// only appears when the SDK has registered it (rubber-duck #8); it is
// not synthesized just because the constant lists it.
func globalFlagSetForCommand(cmd *cobra.Command) *pflag.FlagSet {
	out := pflag.NewFlagSet("", pflag.ContinueOnError)
	out.AddFlagSet(cmd.InheritedFlags())
	for _, name := range nonPersistentGlobalFlags {
		if f := cmd.LocalFlags().Lookup(name); f != nil {
			// AddFlag is a no-op when a flag with the same name already
			// exists in the set, so an inherited-with-same-name case
			// stays a single entry.
			if out.Lookup(name) == nil {
				out.AddFlag(f)
			}
		}
	}
	return out
}
