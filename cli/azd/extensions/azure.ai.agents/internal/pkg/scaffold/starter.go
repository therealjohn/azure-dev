// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// Package scaffold writes embedded starter assets (azure.yaml, infra/...) to
// a user's working directory during `azd ai agent init`. It replaces the
// old GitHub-fetching code path: the bytes now come from the binary via
// //go:embed and never touch the network.
//
// Collision UX matches the previous on-the-wire scaffolder:
//
//   - Build a sorted file list against the target dir, marking each entry
//     as "new" (green +) or "collides" (yellow !).
//   - If any file collides, prompt overwrite / skip / cancel.
//   - Otherwise prompt a single "Initialize the starter template?" confirm.
//   - Honor --no-prompt (AZD_NO_PROMPT): empty dir -> write without
//     prompting; any collision -> structured error (no silent overwrite or
//     silent skip).
package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"github.com/fatih/color"

	"azureaiagent/internal/exterrors"
	"azureaiagent/resources"
)

// StarterFile describes one embedded file destined for the user's
// workspace. Exported so tests can assert the manifest.
type StarterFile struct {
	// Path is the slash-separated path relative to the target directory
	// (e.g. "azure.yaml", "infra/main.bicep", "infra/modules/vnet.bicep").
	Path string

	// Bytes is the embedded file content (UTF-8, LF-normalized).
	Bytes []byte
}

// StarterOptions controls scaffolding behavior. Callers populate this from
// the extension's init flags.
type StarterOptions struct {
	// TargetDir is the destination directory. Files are written relative to
	// this directory, preserving their slash-separated paths.
	TargetDir string

	// NoPrompt suppresses interactive prompts. With NoPrompt set:
	//   - empty target dir (no collisions): write all files without
	//     confirmation.
	//   - any collision: return a Validation error. Callers should not
	//     silently overwrite or skip in non-interactive mode.
	NoPrompt bool

	// Inline switches the UX to a streamlined mode intended for callers
	// that already committed the user to scaffolding (e.g. the manifest
	// flow that just validated an agent template). Behavior:
	//   - No "The following files will be created" preview block.
	//   - No "Initialize the starter template?" confirm prompt.
	//   - Header "Initializing project files:" then one green-plus line
	//     per file as it is written.
	//   - Collision handling is unchanged: the colliding paths are listed
	//     so the user can make an informed overwrite/skip/cancel choice
	//     (or, in NoPrompt mode, the call still returns a Validation
	//     error).
	Inline bool
}

// ScaffoldStarter walks the embedded `azd-ai-starter-basic` payload
// (azure.yaml + infra/) and writes it to opts.TargetDir, with collision
// detection and the same prompts the old GitHub-fetching scaffolder used.
//
// Returns a Cancelled error if the user declines, a Validation error for
// no-prompt collisions, and Dependency / Internal errors for I/O failures.
func ScaffoldStarter(ctx context.Context, azdClient *azdext.AzdClient, opts StarterOptions) error {
	files, err := starterFiles()
	if err != nil {
		return exterrors.Internal(
			exterrors.CodeScaffoldTemplateFailed,
			fmt.Sprintf("loading embedded starter assets: %s", err),
		)
	}

	if opts.TargetDir == "" {
		opts.TargetDir = "."
	}

	// Decorate with collision info against the target dir.
	type entry struct {
		file     StarterFile
		collides bool
	}
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		localPath := filepath.Join(opts.TargetDir, filepath.FromSlash(f.Path))
		_, statErr := os.Stat(localPath)
		entries = append(entries, entry{file: f, collides: statErr == nil})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		return strings.Compare(a.file.Path, b.file.Path)
	})

	collisions := 0
	for _, e := range entries {
		if e.collides {
			collisions++
		}
	}

	if !opts.Inline {
		// Display the file list (matches the old scaffoldTemplate output).
		fmt.Print("\nThe following files will be created from the starter template:\n\n")
		for _, e := range entries {
			if e.collides {
				fmt.Printf("  %s  %s\n", color.YellowString("!"), color.YellowString(e.file.Path))
			} else {
				fmt.Printf("  %s  %s\n", color.GreenString("+"), color.GreenString(e.file.Path))
			}
		}
		fmt.Println()
	} else if collisions > 0 {
		// Inline mode normally skips the preview, but still surface the
		// colliding paths so the user can answer the overwrite prompt
		// with full context.
		fmt.Println()
		for _, e := range entries {
			if e.collides {
				fmt.Printf("  %s  %s\n", color.YellowString("!"), color.YellowString(e.file.Path))
			}
		}
		fmt.Println()
	}

	overwriteCollisions := false
	switch {
	case collisions > 0 && opts.NoPrompt:
		return exterrors.Validation(
			exterrors.CodeScaffoldTemplateFailed,
			fmt.Sprintf("%d file(s) already exist and would be overwritten by the starter template", collisions),
			"re-run without --no-prompt to choose how to handle existing files, "+
				"or move them out of the way and retry",
		)

	case collisions > 0:
		fmt.Printf("%s %d file(s) already exist and would be overwritten.\n\n",
			color.YellowString("Warning:"), collisions)

		conflictChoices := []*azdext.SelectChoice{
			{Label: "Overwrite existing files", Value: "overwrite"},
			{Label: "Skip existing files (keep my versions)", Value: "skip"},
			{Label: "Cancel", Value: "cancel"},
		}
		conflictResp, err := azdClient.Prompt().Select(ctx, &azdext.SelectRequest{
			Options: &azdext.SelectOptions{
				Message: "How would you like to handle existing files?",
				Choices: conflictChoices,
			},
		})
		if err != nil {
			if exterrors.IsCancellation(err) {
				return exterrors.Cancelled("starter template scaffolding was cancelled")
			}
			return exterrors.Dependency(
				exterrors.CodePromptFailed,
				fmt.Sprintf("prompting for conflict resolution: %s", err),
				"",
			)
		}
		switch conflictChoices[*conflictResp.Value].Value {
		case "overwrite":
			overwriteCollisions = true
		case "skip":
			overwriteCollisions = false
		case "cancel":
			return exterrors.Cancelled("starter template scaffolding was cancelled")
		}

	case !opts.NoPrompt && !opts.Inline:
		confirmResp, err := azdClient.Prompt().Confirm(ctx, &azdext.ConfirmRequest{
			Options: &azdext.ConfirmOptions{
				Message:      "Initialize the starter template?",
				DefaultValue: boolPtr(true),
			},
		})
		if err != nil {
			if exterrors.IsCancellation(err) {
				return exterrors.Cancelled("starter template scaffolding was cancelled")
			}
			return exterrors.Dependency(
				exterrors.CodePromptFailed,
				fmt.Sprintf("prompting for confirmation: %s", err),
				"",
			)
		}
		if !*confirmResp.Value {
			return exterrors.Cancelled("starter template scaffolding was cancelled")
		}
	}

	if opts.Inline {
		fmt.Println(output.WithGrayFormat("Initializing project files..."))
	}

	// Write files. We always write non-colliding files; colliding files are
	// only written when the user chose "overwrite".
	written := 0
	// In Inline mode we collapse the per-file output to top-level entries
	// (e.g. "azure.yaml" and "infra/") so the block stays compact next to
	// the agent sample-file download. shown tracks which display paths we
	// have already printed.
	shown := map[string]bool{}
	for _, e := range entries {
		if e.collides && !overwriteCollisions {
			continue
		}
		localPath := filepath.Join(opts.TargetDir, filepath.FromSlash(e.file.Path))
		if err := writeFile(localPath, e.file.Bytes); err != nil {
			return exterrors.Internal(
				exterrors.CodeScaffoldTemplateFailed,
				fmt.Sprintf("writing %s: %s", e.file.Path, err),
			)
		}
		if opts.Inline {
			display := topLevelDisplayPath(e.file.Path)
			if !shown[display] {
				shown[display] = true
				// Match the "Downloading sample..." per-file output:
				// gray, two-space indent, no prefix glyph.
				fmt.Println(output.WithGrayFormat("  %s", display))
			}
		}
		written++
	}

	if !opts.Inline {
		skipped := len(entries) - written
		if skipped > 0 {
			fmt.Printf("  Template initialized: %d file(s) written, %d file(s) skipped.\n", written, skipped)
		} else {
			fmt.Printf("  Template initialized: %d file(s) written.\n", written)
		}
	}
	return nil
}

// topLevelDisplayPath returns the display string used in inline mode for a
// slash-separated path: a file at the root prints as-is ("azure.yaml"),
// any nested path collapses to its top-level directory plus a trailing
// slash ("infra/main.bicep" -> "infra/").
func topLevelDisplayPath(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i] + "/"
	}
	return p
}

// starterFiles returns every embedded starter asset in deterministic order.
// Exported via StarterManifest for tests; kept package-private here because
// callers should go through ScaffoldStarter.
func starterFiles() ([]StarterFile, error) {
	files := []StarterFile{
		{Path: "azure.yaml", Bytes: resources.StarterAzureYaml},
	}

	err := fs.WalkDir(resources.StarterInfra, "starter/infra", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(resources.StarterInfra, p)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", p, readErr)
		}
		// Strip the "starter/" prefix so the destination path is
		// "infra/..." not "starter/infra/...".
		rel := strings.TrimPrefix(p, "starter/")
		// Defense-in-depth: refuse anything that escapes the destination.
		cleaned := path.Clean(rel)
		if path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("invalid embedded path: %s", p)
		}
		files = append(files, StarterFile{Path: cleaned, Bytes: data})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(files) <= 1 {
		return nil, errors.New("embedded starter payload is empty")
	}
	return files, nil
}

// StarterManifest returns the embedded asset list. Used by tests to assert
// the exact file set baked into the binary so accidental additions
// (generated artifacts) or omissions (missing modules) fail loudly.
func StarterManifest() ([]StarterFile, error) {
	return starterFiles()
}

func writeFile(localPath string, content []byte) error {
	dir := filepath.Dir(localPath)
	if dir != "." {
		//nolint:gosec // scaffolded directories are intended to be readable/traversable
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	//nolint:gosec // scaffolded files should remain readable by project tooling
	return os.WriteFile(localPath, content, 0644)
}

func boolPtr(v bool) *bool { return &v }
