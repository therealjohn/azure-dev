// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// init_preflow.go implements the agent-driven onboarding pre-flow that
// runs at the very top of `azd ai agent init` in interactive mode.
//
// Flow (locked in design pass, see plan.md Phase 7):
//
//   Q1 [Confirm]  Do you want your coding agent to set up and create an
//                 agent in Microsoft Foundry?
//                   No  -> return (handled=false) -> existing init runs.
//                   Yes -> continue.
//
//   Q2 [Confirm]  Install the AZD AI skill for your coding
//                 agent?
//                   Yes -> Q3 -> install
//                   No  -> skip install, go to starter prompt
//
//   Q3 [Select]   Which coding agent are you using?
//                 (claude / codex / gemini / copilot / opencode / custom)
//                 custom -> prompt for path
//
//   Install       Shell out to `azd ai doc skills install ...`. If the
//                 docs extension is missing, offer to install it first.
//
//   Render        Print the starter prompt, optionally copy it to the
//                 system clipboard, show a tool-specific "you're ready
//                 to go" block.
//
//   Return        (handled=true) -- caller skips the existing init flow.

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/fatih/color"
)

// docsExtensionID is the canonical ID of the docs front-door extension
// that owns `azd ai doc skills install`. Kept as a constant so the
// install-detection helper and the dispatch helper agree on the spelling.
const docsExtensionID = "azure.ai.docs"

// preflowTarget mirrors a built-in target choice in the docs install
// command, with the tool-friendly extras the pre-flow needs:
// displayLabel (shown in the Select choice list, with gray-colored
// path) and pasteInstruction (used in the ready-to-go block).
type preflowTarget struct {
	// targetValue is the --target argument passed to
	// `azd ai doc skills install` (e.g. "copilot").
	targetValue string
	// displayName is the tool's user-facing name (e.g. "GitHub Copilot").
	displayName string
	// installPath is the relative directory the install writes into.
	// Empty for "custom" -- user provides via the follow-up prompt.
	installPath string
	// pasteInstruction is the per-tool sentence in the ready-to-go
	// block (e.g. "Open GitHub Copilot Chat and paste the prompt.").
	pasteInstruction string
}

// preflowTargets is the ordered list of choices shown in Q3. Order
// drives both the Select option order and the help text. Matches
// the targets table in azure.ai.docs' skill_install.go.
var preflowTargets = []preflowTarget{
	{
		targetValue:      "claude",
		displayName:      "Claude Code",
		installPath:      ".claude/skills/azd-ai-skill",
		pasteInstruction: "Open Claude Code and paste the prompt.",
	},
	{
		targetValue:      "codex",
		displayName:      "Codex",
		installPath:      ".agents/skills/azd-ai-skill",
		pasteInstruction: "Open Codex CLI and paste the prompt.",
	},
	{
		targetValue:      "gemini",
		displayName:      "Gemini CLI",
		installPath:      ".agents/skills/azd-ai-skill",
		pasteInstruction: "Open Gemini CLI and paste the prompt.",
	},
	{
		targetValue:      "copilot",
		displayName:      "GitHub Copilot",
		installPath:      ".agents/skills/azd-ai-skill",
		pasteInstruction: "Open GitHub Copilot Chat and paste the prompt.",
	},
	{
		targetValue:      "opencode",
		displayName:      "Opencode",
		installPath:      ".agents/skills/azd-ai-skill",
		pasteInstruction: "Open Opencode and paste the prompt.",
	},
	{
		targetValue:      "custom",
		displayName:      "Custom path",
		installPath:      "",
		pasteInstruction: "Open your coding agent and paste the prompt.",
	},
}

// InitPreflowAction is the action object the cobra RunE constructs and
// calls when in interactive mode (matches the action-object pattern per
// trangevi's PR feedback / sample_list.go / show.go / etc.).
type InitPreflowAction struct {
	out       io.Writer
	azdClient *azdext.AzdClient
	runner    azdRunner
	// cwd is the working directory used both for rendering the starter
	// prompt (ProjectPath substitution) and as the implicit root for
	// install paths.
	cwd string
	// copyClip copies text to the system clipboard. Returns the
	// 3-valued outcome (Copied / Skipped / Failed). Injected so tests
	// can drive every branch deterministically.
	copyClip func(text string) ClipboardOutcome
}

// Run executes the pre-flow. Returns (handled, err) where:
//   - handled == true: the user delegated to a coding agent; the caller
//     MUST skip the existing InitAction so we do not double-prompt.
//   - handled == false: the user declined Q1; the caller proceeds with
//     the existing init flow unchanged.
func (a *InitPreflowAction) Run(ctx context.Context) (bool, error) {
	delegate, err := a.askDelegate(ctx)
	if err != nil {
		return false, err
	}
	if !delegate {
		// Q1=No -- existing init takes over. The caller checks the
		// handled bool, so returning err=nil here is correct.
		return false, nil
	}

	// From here on we own the flow regardless of errors; always return
	// handled=true so the caller skips InitAction.

	// chosen tracks the tool the user picked at Q3. When Q2=No (no
	// install) we never run Q3 -- fall back to the "custom" copy in the
	// ready-to-go block since we cannot name a specific tool then.
	//
	// We MUST track the chosen target directly rather than recover it
	// from the install path because codex/gemini/copilot/opencode all
	// install to the same path (.agents/skills/azd-ai-skill); a
	// reverse-lookup by path would always resolve to the first matching
	// entry (codex), producing wrong "Open Codex CLI ..." text even
	// when the user selected GitHub Copilot.
	chosen := preflowTargets[len(preflowTargets)-1] // "custom" default

	var installedAt string
	wantInstall, err := a.askInstallSkill(ctx)
	if err != nil {
		return true, err
	}
	if wantInstall {
		target, customPath, err := a.askTargetTool(ctx)
		if err != nil {
			return true, err
		}
		chosen = target
		path, err := a.installSkill(ctx, target, customPath)
		if err != nil {
			return true, err
		}
		installedAt = path
	}

	body, err := renderStarterPrompt(StarterPromptVars{
		ProjectPath: a.cwd,
		SkillPath:   installedAt,
	})
	if err != nil {
		return true, fmt.Errorf("render starter prompt: %w", err)
	}

	printStarterPrompt(a.out, body)
	a.handleClipboard(ctx, body)
	a.printReadyToGo(chosen, installedAt)

	return true, nil
}

// askDelegate is Q1. Default value is "No" so the existing init flow is
// the path of least surprise for users who just hit enter.
func (a *InitPreflowAction) askDelegate(ctx context.Context) (bool, error) {
	resp, err := a.azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
		Options: &azdext.ConfirmOptions{
			Message:      "Do you want your coding agent to set up and create an agent in Microsoft Foundry?",
			DefaultValue: new(false),
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return false, exterrors.Cancelled("initialization was cancelled")
		}
		return false, fmt.Errorf("prompt delegate-to-agent: %w", err)
	}
	if resp == nil || resp.Value == nil {
		return false, nil
	}
	return *resp.Value, nil
}

// askInstallSkill is Q2. Default value is "Yes" -- if the user said
// yes to Q1 it's a strong signal they want the skill installed.
func (a *InitPreflowAction) askInstallSkill(ctx context.Context) (bool, error) {
	resp, err := a.azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
		Options: &azdext.ConfirmOptions{
			Message:      "Install the AZD AI skill for your coding agent?",
			DefaultValue: new(true),
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return false, exterrors.Cancelled("initialization was cancelled")
		}
		return false, fmt.Errorf("prompt install-skill: %w", err)
	}
	if resp == nil || resp.Value == nil {
		return false, nil
	}
	return *resp.Value, nil
}

// askTargetTool is Q3. Returns the chosen target plus, for "custom", the
// resolved relative path the user typed.
func (a *InitPreflowAction) askTargetTool(ctx context.Context) (preflowTarget, string, error) {
	choices := make([]*azdext.SelectChoice, len(preflowTargets))
	for i, t := range preflowTargets {
		choices[i] = &azdext.SelectChoice{
			Value: t.targetValue,
			Label: targetSelectLabel(t),
		}
	}

	resp, err := a.azdClient.Prompt().Select(ctx, &azdext.SelectRequest{
		Options: &azdext.SelectOptions{
			Message: "Which coding agent are you using?",
			Choices: choices,
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return preflowTarget{}, "", exterrors.Cancelled("initialization was cancelled")
		}
		return preflowTarget{}, "", fmt.Errorf("prompt coding-agent target: %w", err)
	}
	if resp == nil || resp.Value == nil {
		return preflowTarget{}, "", fmt.Errorf("no target selected")
	}

	chosen := preflowTargets[int(*resp.Value)]

	if chosen.targetValue != "custom" {
		return chosen, "", nil
	}

	pathResp, err := a.azdClient.Prompt().Prompt(ctx, &azdext.PromptRequest{
		Options: &azdext.PromptOptions{
			Message:     "Custom install path (relative to current directory):",
			HelpMessage: "Example: .my-tool/skills/foundry",
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return preflowTarget{}, "", exterrors.Cancelled("initialization was cancelled")
		}
		return preflowTarget{}, "", fmt.Errorf("prompt custom install path: %w", err)
	}
	if pathResp == nil {
		return preflowTarget{}, "", fmt.Errorf("no custom install path provided")
	}
	customPath := strings.TrimSpace(pathResp.Value)
	if customPath == "" {
		return preflowTarget{}, "", fmt.Errorf("custom install path must not be empty")
	}
	return chosen, customPath, nil
}

// targetSelectLabel renders a Q3 Select choice label: tool name first,
// path in gray after, e.g. "GitHub Copilot (.agents/skills/azd-ai-skill)".
// Matches the look of azd's `WithGrayFormat` convention.
func targetSelectLabel(t preflowTarget) string {
	if t.installPath == "" {
		return t.displayName
	}
	return fmt.Sprintf("%s %s", t.displayName, output.WithGrayFormat("("+t.installPath+")"))
}

// installSkill performs the actual install via the docs front-door
// extension. Returns the install path on success (used for the starter
// prompt's SkillPath substitution).
func (a *InitPreflowAction) installSkill(ctx context.Context, target preflowTarget, customPath string) (string, error) {
	if err := a.ensureDocsExtension(ctx); err != nil {
		return "", err
	}

	args := []string{"ai", "doc", "skills", "install",
		"--target", target.targetValue,
		"--no-prompt",
		"--output", "json",
	}
	if target.targetValue == "custom" {
		args = append(args, "--path", customPath)
	}

	// Capture the child's stdout so we can parse the JSON install
	// receipt. Errors from the child are written to its own os.Stderr;
	// passing nil to runner.Run forwards stderr to the parent terminal
	// so any failure detail is visible to the user live.
	var stdout strings.Builder
	if err := a.runner.Run(ctx, args, &stdout, nil); err != nil {
		// We pre-checked docs-extension presence in ensureDocsExtension
		// above (see ext_lookup.go for the rationale on why we don't
		// rely on azd's auto-install). Any error here is from the
		// install command itself; wrap and re-raise.
		return "", fmt.Errorf("run `azd ai doc skills install`: %w", err)
	}

	var result skillInstallReceipt
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		// No JSON -> degrade to "we don't know the path" rather than fail.
		if target.targetValue == "custom" {
			return customPath, nil
		}
		return target.installPath, nil
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		// JSON parse failure is not fatal -- the install itself
		// succeeded (exit 0). Fall back to the declared path.
		if target.targetValue == "custom" {
			return customPath, nil
		}
		return target.installPath, nil
	}
	if result.Path != "" {
		return result.Path, nil
	}
	if target.targetValue == "custom" {
		return customPath, nil
	}
	return target.installPath, nil
}

// skillInstallReceipt mirrors the JSON wire shape emitted by
// `azd ai doc skills install --output json`. Decoupled from the
// azure.ai.docs source struct so the two extensions can ship
// independently without cross-extension type imports.
type skillInstallReceipt struct {
	Status string   `json:"status"`
	Target string   `json:"target"`
	Path   string   `json:"path"`
	Files  []string `json:"files"`
}

// ensureDocsExtension verifies that azure.ai.docs is installed. When it
// is not, prompts the user to install it and shells out to
// `azd ext install azure.ai.docs` on confirm. Returns an error explaining
// what to run when the user declines.
func (a *InitPreflowAction) ensureDocsExtension(ctx context.Context) error {
	lookup, err := lookupExtension(ctx, a.runner, docsExtensionID)
	if err != nil {
		// Lookup failure is treated as a soft warning rather than a hard
		// stop: the install dispatch below may still work (e.g. the user
		// installed via an unusual source). The shell-out's own error
		// surfaces if the dispatch really does fail.
		fmt.Fprintf(a.out, "%s could not check whether %s is installed: %v\n",
			color.YellowString("warning:"), docsExtensionID, err)
		return nil
	}
	if lookup.Installed {
		return nil
	}

	resp, err := a.azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
		Options: &azdext.ConfirmOptions{
			Message: fmt.Sprintf(
				"The %s extension is required. Install it now?", docsExtensionID),
			DefaultValue: new(true),
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return exterrors.Cancelled("initialization was cancelled")
		}
		return fmt.Errorf("prompt install %s: %w", docsExtensionID, err)
	}

	if resp == nil || resp.Value == nil || !*resp.Value {
		return fmt.Errorf(
			"%s is not installed. Run `azd ext install %s` and re-try",
			docsExtensionID, docsExtensionID)
	}

	fmt.Fprintln(a.out)
	fmt.Fprintf(a.out, "Installing %s...\n", docsExtensionID)
	if err := installExtension(ctx, a.runner, docsExtensionID, a.out, a.out); err != nil {
		return fmt.Errorf("auto-install %s: %w", docsExtensionID, err)
	}
	return nil
}

// handleClipboard offers to copy the prompt to the system clipboard
// when the environment looks interactive, and prints the right
// follow-up message in every outcome (copied / skipped / failed /
// user-declined).
func (a *InitPreflowAction) handleClipboard(ctx context.Context, body string) {
	// Pre-check the environment. When we know clipboard access is
	// impossible (CI, headless Linux, SSH, etc.), skip the confirm
	// prompt entirely -- asking would only confuse the user.
	if env := (osClipboardEnv{}); isHeadlessEnv(env) {
		fmt.Fprintln(a.out, output.WithGrayFormat(
			"Copy the prompt above manually -- no clipboard available in this environment."))
		fmt.Fprintln(a.out)
		return
	}

	resp, err := a.azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
		Options: &azdext.ConfirmOptions{
			Message:      "Copy prompt to clipboard?",
			DefaultValue: new(true),
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			// Cancellation here is non-fatal -- we already printed the
			// prompt, the user can copy it manually.
			fmt.Fprintln(a.out)
			return
		}
		fmt.Fprintln(a.out, output.WithGrayFormat(
			"Skipped clipboard copy. Copy the prompt above manually."))
		fmt.Fprintln(a.out)
		return
	}
	if resp == nil || resp.Value == nil || !*resp.Value {
		fmt.Fprintln(a.out, output.WithGrayFormat(
			"OK -- copy the prompt above manually when you're ready."))
		fmt.Fprintln(a.out)
		return
	}

	switch a.copyClip(body) {
	case ClipboardCopied:
		fmt.Fprintln(a.out, output.WithSuccessFormat("The prompt is copied to your clipboard!"))
	case ClipboardSkipped:
		// Belt-and-suspenders: handleClipboard pre-checked, but if the
		// helper still reports Skipped (e.g. the env changed mid-run),
		// soft-fail with the same message.
		fmt.Fprintln(a.out, output.WithGrayFormat(
			"Copy the prompt above manually -- no clipboard available in this environment."))
	case ClipboardFailed:
		fmt.Fprintln(a.out, output.WithGrayFormat(
			"Could not access the clipboard -- copy the prompt above manually."))
	}
	fmt.Fprintln(a.out)
}

// printReadyToGo writes the tool-specific "You're ready to go!" block.
// The block is the final thing the user sees from azd before they paste
// the prompt into their coding agent.
//
//   - Bold yellow header                          ("You're ready to go!")
//   - Paste instruction tailored to the target    ("Open Claude Code ...")
//   - What the agent will do                      (short narrative)
//   - Prefer-to-set-up-manually fallback          (azd commands)
//   - Docs link                                   (azd ai doc agent)
//
// When installedAt is empty (user declined Q2), the paste instruction
// drops the install reference but keeps the rest of the block intact so
// the user still has the docs link and manual-fallback commands.
func (a *InitPreflowAction) printReadyToGo(target preflowTarget, installedAt string) {
	bold := color.New(color.FgYellow, color.Bold)
	fmt.Fprintln(a.out, bold.Sprint("You're ready to go!"))
	fmt.Fprintln(a.out)

	fmt.Fprintln(a.out, color.New(color.Bold).Sprint(target.pasteInstruction))
	fmt.Fprintln(a.out)

	if installedAt != "" {
		fmt.Fprintln(a.out, output.WithGrayFormat("Your agent will use the AZD AI skill at %s", installedAt))
		fmt.Fprintln(a.out, output.WithGrayFormat("to scaffold, provision, and deploy a Foundry agent tailored"))
		fmt.Fprintln(a.out, output.WithGrayFormat("to your project."))
	} else {
		fmt.Fprintln(a.out, output.WithGrayFormat("Your agent will follow the starter prompt to scaffold, provision,"))
		fmt.Fprintln(a.out, output.WithGrayFormat("and deploy a Foundry agent tailored to your project."))
	}
	fmt.Fprintln(a.out)

	fmt.Fprintln(a.out, color.New(color.Bold).Sprint("Prefer to set up manually?"))
	fmt.Fprintln(a.out, output.WithGrayFormat("  azd ai agent init             Run the interactive scaffolder yourself."))
	fmt.Fprintln(a.out, output.WithGrayFormat("  azd provision                 Provision Foundry resources."))
	fmt.Fprintln(a.out, output.WithGrayFormat("  azd deploy                    Deploy the agent."))
	fmt.Fprintln(a.out, output.WithGrayFormat("  azd ai agent show             Inspect the deployed agent."))
	fmt.Fprintln(a.out)

	fmt.Fprint(a.out, output.WithGrayFormat("Docs: "))
	fmt.Fprintln(a.out, output.WithLinkFormat("https://aka.ms/azd-ai-agent-docs"))
	fmt.Fprintln(a.out, output.WithGrayFormat("      Or run `azd ai doc agent` for the agent-friendly topic index."))
	fmt.Fprintln(a.out)
}
