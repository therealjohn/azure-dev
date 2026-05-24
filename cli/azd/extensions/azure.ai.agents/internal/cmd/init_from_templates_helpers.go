// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"azureaiagent/internal/exterrors"
	"azureaiagent/internal/pkg/agents/agent_yaml"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/azure/azure-dev/cli/azd/pkg/output"
	"gopkg.in/yaml.v3"
)

const agentTemplatesURL = "https://aka.ms/foundry-agents-samples"

// Template type constants
const (
	// TemplateTypeAgent is a template that points to an agent.yaml manifest file.
	TemplateTypeAgent = "agent"

	// TemplateTypeAzd is a full azd template repository.
	TemplateTypeAzd = "azd"

	// templateTypeExtensionAIAgent is the discriminator value in the unified
	// awesome-azd templates.json manifest that identifies an agent-init
	// template. Entries with any other (or empty) templateType belong to the
	// standard awesome-azd gallery and are filtered out.
	templateTypeExtensionAIAgent = "extension.ai.agent"

	// featuredTag is the extensionTags value that marks a template for the
	// curated starter list. These templates are shown first; the user can
	// expand to see the full catalog.
	featuredTag = "featured"

	// recommendedTag is the extensionTags value that identifies the default
	// pre-selected template in the featured list.
	recommendedTag = "recommended"

	// seeAllSentinel is the SelectChoice.Value used for the "See all
	// templates..." option appended to the featured list.
	seeAllSentinel = "__see_all__"
)

// AgentTemplate represents an agent template entry from the remote JSON catalog.
// Field names mirror the awesome-azd templates.json schema.
type AgentTemplate struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Languages          []string `json:"languages"`
	ExtensionFramework string   `json:"extensionFramework"`
	Source             string   `json:"source"`
	ExtensionTags      []string `json:"extensionTags"`
	TemplateType       string   `json:"templateType"`
}

// EffectiveType determines the template type by inspecting the source URL.
// If it ends with agent.yaml or agent.manifest.yaml, it's an agent manifest.
// Otherwise, it's treated as a full azd template repo.
func (t *AgentTemplate) EffectiveType() string {
	lower := strings.ToLower(t.Source)
	if strings.HasSuffix(lower, "/agent.yaml") ||
		strings.HasSuffix(lower, "/agent.manifest.yaml") ||
		lower == "agent.yaml" ||
		lower == "agent.manifest.yaml" {
		return TemplateTypeAgent
	}
	return TemplateTypeAzd
}

const (
	initModeFromCode = "from_code"
	initModeTemplate = "template"
)

// promptInitMode resolves the init-mode for `azd ai agent init` --
// "use the code in this directory" (initModeFromCode) vs "start new
// from a template" (initModeTemplate). The routing order is:
//
//  1. flags.fromCode set -> initModeFromCode (explicit user/agent intent).
//
//  2. cwd is empty -> initModeTemplate (no code to use; offer templates).
//
//  3. cwd is "bootstrap-only" (only the pre-flow's azure.yaml stub
//     plus housekeeping files) -> initModeFromCode silently. We pick
//     from-code rather than template because:
//
//     a. InitFromCodeAction.ensureProject() correctly reuses the
//     bootstrap azure.yaml instead of re-scaffolding the starter
//     template, so the user's pre-flow setup is honored.
//     b. promptAgentTemplate() also requires interactive mode -- routing
//     bootstrap-only -> initModeTemplate would just defer the
//     --no-prompt failure to a later prompt (rubber-duck #1).
//
//     In interactive mode we print a muted "Detected AZD AI bootstrap
//     files; setting up a new agent." line so the user sees WHY the
//     init-mode prompt did not appear (rubber-duck #9). In --no-prompt
//     mode we stay silent to keep machine logs clean.
//
//  4. cwd is non-empty AND not bootstrap-only AND --no-prompt is set
//     -> deterministic ErrorWithSuggestion. The interactive Select
//     would have no way to resolve in non-interactive mode; surfacing
//     the failure with an actionable suggestion (pass --from-code or
//     --manifest) is better than letting the prompt RPC error out.
//
//  5. Otherwise -> interactive Select prompt (the legacy behavior).
func promptInitMode(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	flags *initFlags,
	out io.Writer,
) (string, error) {
	// 1. Explicit flag wins over any directory-state inference.
	if flags != nil && flags.fromCode {
		return initModeFromCode, nil
	}

	empty, err := dirIsEmpty(".")
	if err != nil {
		return "", fmt.Errorf("checking current directory: %w", err)
	}

	// 2. Empty dir => template flow (legacy behavior preserved).
	if empty {
		return initModeTemplate, nil
	}

	// 3. Bootstrap-only => silently route to from-code so the FOLLOW-UP
	// `azd ai agent init` invocation after the pre-flow does not hit
	// the wrong-shaped Select prompt.
	bootstrap, err := dirIsAgentBootstrapOnly(".")
	if err != nil {
		// Do NOT swallow filesystem errors -- a permission failure
		// here would otherwise route the user through the wrong prompt
		// with no diagnostic.
		return "", fmt.Errorf("checking for AZD AI bootstrap state: %w", err)
	}
	if bootstrap {
		// Surface the silent short-circuit in interactive mode so the
		// user understands WHY they did not see the usual init-mode
		// question. Stay silent in --no-prompt mode.
		if flags != nil && !flags.noPrompt && out != nil {
			fmt.Fprintln(out, output.WithGrayFormat(
				"Detected AZD AI bootstrap files; setting up a new agent."))
		}
		return initModeFromCode, nil
	}

	// 4. Non-empty, non-bootstrap, --no-prompt: bail with a clear
	// suggestion rather than letting the Select RPC fail opaquely.
	if flags != nil && flags.noPrompt {
		return "", exterrors.Validation(
			exterrors.CodePromptFailed,
			"cannot determine init mode in non-interactive mode "+
				"(directory is not empty and not from the AZD AI bootstrap pre-flow)",
			"Pass --from-code to use the existing code, or "+
				"--manifest <path> to use an agent manifest.",
		)
	}

	// 5. Interactive Select (legacy behavior).
	choices := []*azdext.SelectChoice{
		{Label: "Use the code in the current directory", Value: initModeFromCode},
		{Label: "Start new from a template", Value: initModeTemplate},
	}

	defaultIndex := int32(0)

	resp, err := azdClient.Prompt().Select(ctx, &azdext.SelectRequest{
		Options: &azdext.SelectOptions{
			Message:       "How do you want to initialize your agent?",
			Choices:       choices,
			SelectedIndex: &defaultIndex,
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return "", exterrors.Cancelled("initialization mode selection was cancelled")
		}
		return "", fmt.Errorf("failed to prompt for initialization mode: %w", err)
	}

	return choices[*resp.Value].Value, nil
}

// dirIsEmpty reports whether dir contains no entries at all.
func dirIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	return len(entries) == 0, nil
}

// bootstrapOnlyFileWhitelist lists the housekeeping files most projects
// keep at the root that we accept as "bootstrap noise" rather than user
// code. Matched case-insensitively. Anything NOT in this list (or in
// bootstrapOnlyDirWhitelist) makes dirIsAgentBootstrapOnly return false.
//
// Liberal-by-design (see plan.md "Decisions"): the cost of false
// negatives (asking the user a useless prompt) is much higher than the
// cost of false positives (silently routing through the from-code path
// when there's a stray README; the from-code path's noPrompt error
// path still surfaces a clear suggestion).
var bootstrapOnlyFileWhitelist = []string{
	".gitignore",
	".gitattributes",
	".editorconfig",
	".ds_store",
	"readme",
	"readme.md",
	"readme.txt",
	"readme.rst",
	"license",
	"license.md",
	"license.txt",
	"contributing.md",
	"code_of_conduct.md",
	"security.md",
	"changelog.md",
	// azure.yaml is handled separately -- it MUST carry the bootstrap
	// marker AND have no services/infra/hooks before we accept it.
}

// bootstrapOnlyDirWhitelist lists directory names we accept without
// recursing. .claude/ and .agents/ are the well-known skill pack roots;
// the others are editor/CI metadata most repos already have.
//
// We do NOT recurse into these dirs because their contents are by
// definition "noise" relative to the question we're answering: "did the
// user add agent code?".
var bootstrapOnlyDirWhitelist = []string{
	".git", // also matches the .git FILE in worktrees, handled in loop
	".azure",
	".azd",
	".github",
	".vscode",
	".devcontainer",
	".claude",
	".agents",
}

// bootstrapWalkMaxDepth caps the depth at which we look for SKILL.md
// in unknown top-level dirs (custom skill install paths). Keeps the
// helper O(depth^N) bounded on monorepos that happen to satisfy
// dirIsEmpty=false but bootstrap-only=true conditions.
const bootstrapWalkMaxDepth = 4

// azdAiSkillMarker is the string we look for inside any SKILL.md to
// confirm "this is OUR skill" rather than an unrelated SKILL.md a
// different tool wrote. The full frontmatter line is `name: AZD AI`;
// we match the case-insensitive substring so trivial whitespace or
// quote variations don't trip us.
const azdAiSkillMarker = "name: AZD AI"

// dirIsAgentBootstrapOnly reports whether dir contains only files we
// recognize as "bootstrap artifacts": a marker-bearing azure.yaml stub
// plus housekeeping files (.git, README, .vscode/, etc.) plus skill
// install dirs. Returns false when any unknown top-level entry exists
// OR when the azure.yaml lacks the bootstrap marker.
//
// We require AT LEAST ONE bootstrap signal (the marker-bearing
// azure.yaml) -- a directory with only .git/ and a README is NOT
// bootstrap-only, it's just an empty repo, and the caller's dirIsEmpty
// check should have routed it differently in the first place.
//
// Real I/O errors are propagated, not swallowed (rubber-duck #7) --
// degrading to "false" on EACCES could route a permission-broken repo
// through the wrong-shaped prompt with no diagnostic.
func dirIsAgentBootstrapOnly(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read directory %s: %w", dir, err)
	}
	if len(entries) == 0 {
		// Caller should have used dirIsEmpty for this case. Returning
		// false here makes the contract explicit: bootstrap-only
		// REQUIRES the marker.
		return false, nil
	}

	var foundBootstrapMarker bool

	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)

		// Symlink safety: never follow a symlink that resolves outside
		// of dir. We compare the symlink's EvalSymlinks result against
		// dir using filepath.Rel containment, mirroring the safety
		// pattern used by azure.ai.docs' validateCustomPath.
		info, err := os.Lstat(full)
		if err != nil {
			return false, fmt.Errorf("lstat %s: %w", full, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !symlinkResolvesUnder(full, dir) {
				// A symlinked .github/ pointing to /etc => not safe to
				// classify as bootstrap-only.
				return false, nil
			}
			// Even when the symlink target is inside dir, we treat the
			// link itself as "unknown" unless its name matches the
			// whitelist below. Fall through to the name checks.
		}

		// azure.yaml is the load-bearing signal -- if and only if it
		// carries the bootstrap marker AND has no services/infra/hooks.
		if strings.EqualFold(name, bootstrapAzureYamlName) {
			ok, parseErr := azureYamlIsBootstrapStub(full)
			if parseErr != nil {
				return false, parseErr
			}
			if !ok {
				return false, nil
			}
			foundBootstrapMarker = true
			continue
		}

		// File whitelist (case-insensitive).
		if !e.IsDir() && nameMatchesAny(name, bootstrapOnlyFileWhitelist) {
			continue
		}

		// Directory whitelist (case-insensitive). The .git ENTRY can
		// be a file in git worktrees -- both shapes accepted.
		if nameMatchesAny(name, bootstrapOnlyDirWhitelist) {
			continue
		}

		// Unknown top-level entry. For directories, fall back to the
		// custom-skill-path probe: walk into it looking for our SKILL.md
		// (rubber-duck #3). If we find it, the dir counts as bootstrap
		// noise. Anything else, or a non-directory unknown file, fails.
		if !e.IsDir() {
			return false, nil
		}
		hasSkill, walkErr := dirContainsAzdAiSkill(full, bootstrapWalkMaxDepth)
		if walkErr != nil {
			return false, walkErr
		}
		if !hasSkill {
			return false, nil
		}
	}

	return foundBootstrapMarker, nil
}

// nameMatchesAny reports whether name matches any entry in the list,
// case-insensitively. Hoisted so the matching logic is a single line
// in the caller rather than an inline ToLower comparison per check.
func nameMatchesAny(name string, list []string) bool {
	lower := strings.ToLower(name)
	return slices.Contains(list, lower)
}

// symlinkResolvesUnder reports whether a symlink's resolved target sits
// inside (or equals) root. False on any EvalSymlinks failure -- we
// fail closed because an unresolvable link is not safe to whitelist.
func symlinkResolvesUnder(link, root string) bool {
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absResolved)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// azureYamlIsBootstrapStub returns true when path is a valid azure.yaml
// whose metadata.template marker is `azd-ai-bootstrap@*` AND which has
// no `services:` / `infra:` / `hooks:` declared.
//
// The no-services constraint (rubber-duck #5) is what stops a real
// project from being misclassified after the user runs through the
// normal `azd ai agent init` flow: addToProject populates `services:`,
// at which point this returns false even though the marker may still
// be present.
func azureYamlIsBootstrapStub(path string) (bool, error) {
	//nolint:gosec // path is a fixed filename inside the user's cwd
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	// Decode into a permissive map so unknown future keys don't make
	// us reject a legitimate stub. We only care about the four fields
	// below.
	var doc struct {
		Metadata struct {
			Template string `yaml:"template"`
		} `yaml:"metadata"`
		Services map[string]any `yaml:"services"`
		Infra    any            `yaml:"infra"`
		Hooks    any            `yaml:"hooks"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// Malformed YAML => not a bootstrap stub. Don't surface the
		// parse error: this isn't an I/O failure, it's user-supplied
		// content the caller can route the normal way.
		return false, nil
	}

	if !strings.HasPrefix(doc.Metadata.Template, bootstrapTemplatePrefix+"@") {
		return false, nil
	}
	if len(doc.Services) > 0 {
		return false, nil
	}
	if doc.Infra != nil {
		return false, nil
	}
	if doc.Hooks != nil {
		return false, nil
	}
	return true, nil
}

// dirContainsAzdAiSkill returns true when any SKILL.md found under root
// (up to depthLimit subdirectories deep) contains the azdAiSkillMarker. This
// is how we detect custom skill install paths the user supplied to the
// pre-flow's Q3.
func dirContainsAzdAiSkill(root string, depthLimit int) (bool, error) {
	rootDepth := pathDepth(root)
	var found bool
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip subtrees we can't read rather than failing the entire
			// bootstrap-only check: an EACCES on a single subdir is
			// "no skill found here", not a fatal classification error.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if pathDepth(path)-rootDepth > depthLimit {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		//nolint:gosec // path is below the user's cwd, walked from a controlled root
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(body)), strings.ToLower(azdAiSkillMarker)) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		// WalkDir only returns the err that callbacks did not consume;
		// since we swallow per-entry errors above, anything bubbling
		// here is unusual enough to surface.
		return false, fmt.Errorf("walk %s for SKILL.md: %w", root, walkErr)
	}
	return found, nil
}

// pathDepth counts the number of separators in a cleaned path. Used by
// dirContainsAzdAiSkill to enforce the depth cap.
func pathDepth(p string) int {
	cleaned := filepath.Clean(p)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return 0
	}
	return strings.Count(cleaned, string(filepath.Separator))
}

// fetchAgentTemplates retrieves the agent template catalog from the remote
// awesome-azd manifest URL.
func fetchAgentTemplates(ctx context.Context, httpClient *http.Client) ([]AgentTemplate, error) {
	return fetchAgentTemplatesFromURL(ctx, httpClient, agentTemplatesURL)
}

// fetchAgentTemplatesFromURL retrieves the awesome-azd templates manifest from
// the given URL and returns only entries whose templateType marks them as
// agent-init templates. The URL is parameterized to keep this function
// directly testable against an httptest server.
func fetchAgentTemplatesFromURL(
	ctx context.Context,
	httpClient *http.Client,
	url string,
) ([]AgentTemplate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agent templates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch agent templates: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent templates response: %w", err)
	}

	var all []AgentTemplate
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, fmt.Errorf("failed to parse agent templates: %w", err)
	}

	// Keep only agent-init entries. The shared templates.json manifest also
	// carries the awesome-azd gallery; those entries must not surface here.
	filtered := make([]AgentTemplate, 0, len(all))
	for _, t := range all {
		if t.TemplateType == templateTypeExtensionAIAgent {
			filtered = append(filtered, t)
		}
	}

	// Always emit the fetched/matched counts to make transition-period and
	// misconfiguration issues debuggable.
	log.Printf(
		"agent templates manifest: fetched %d templateType=%q (source=%s)",
		len(filtered), templateTypeExtensionAIAgent, url,
	)

	// If we received entries but filtered them all out, the manifest is
	// almost certainly in the legacy format or the discriminator value has
	// changed. Surface that explicitly instead of returning an empty list,
	// which the caller cannot distinguish from an intentionally empty manifest.
	if len(all) > 0 && len(filtered) == 0 {
		return nil, fmt.Errorf(
			"agent templates manifest at %s contained %d entries but none had templateType=%q",
			url, len(all), templateTypeExtensionAIAgent,
		)
	}

	return filtered, nil
}

// isFeatured reports whether the template carries the "featured" extensionTag,
// which marks it for the curated starter list.
func (t *AgentTemplate) isFeatured() bool {
	return slices.Contains(t.ExtensionTags, featuredTag)
}

// isRecommended reports whether the template carries the "recommended"
// extensionTag, which marks it as the default pre-selected template.
func (t *AgentTemplate) isRecommended() bool {
	return slices.Contains(t.ExtensionTags, recommendedTag)
}

// promptAgentTemplate guides the user through language selection and template selection.
// Returns the selected AgentTemplate. The caller should check EffectiveType() to determine
// whether to use the agent.yaml manifest flow or the full azd template flow.
//
// Templates tagged "featured" are shown first in a curated list. The template
// tagged "recommended" gets a (Recommended) suffix in the label and is
// pre-selected. A "See all templates..." option expands to the full catalog.
func promptAgentTemplate(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	httpClient *http.Client,
	noPrompt bool,
) (*AgentTemplate, error) {
	if noPrompt {
		return nil, exterrors.Validation(
			exterrors.CodePromptFailed,
			"template selection requires interactive mode",
			"run 'azd ai agent sample list --output json' to discover available templates, "+
				"then rerun 'azd ai agent init -m <manifestUrl>' (or 'azd init -t <repoUrl>' for full template repos)",
		)
	}

	fmt.Println(output.WithGrayFormat("Retrieving agent templates..."))

	templates, err := fetchAgentTemplates(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve agent templates: %w", err)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no agent templates available")
	}

	// Prompt for language. Values must match the language tokens used in
	// the awesome-azd templates.json `languages` field (e.g. "dotnetCsharp").
	languageChoices := []*azdext.SelectChoice{
		{Label: "Python", Value: "python"},
		{Label: "C#", Value: "dotnetCsharp"},
	}

	langResp, err := azdClient.Prompt().Select(ctx, &azdext.SelectRequest{
		Options: &azdext.SelectOptions{
			Message: "Select a language",
			Choices: languageChoices,
		},
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return nil, exterrors.Cancelled("language selection was cancelled")
		}
		return nil, fmt.Errorf("failed to prompt for language: %w", err)
	}

	selectedLanguage := languageChoices[*langResp.Value].Value

	// Filter templates by selected language (entries can declare multiple).
	langFiltered := make([]AgentTemplate, 0, len(templates))
	for _, t := range templates {
		if slices.Contains(t.Languages, selectedLanguage) {
			langFiltered = append(langFiltered, t)
		}
	}

	if len(langFiltered) == 0 {
		return nil, fmt.Errorf(
			"no agent templates available for %s",
			languageChoices[*langResp.Value].Label,
		)
	}

	// Partition into featured vs rest.
	featured, rest := partitionFeatured(langFiltered)

	// When there are both featured and non-featured templates, show the
	// curated featured list first with a "See all templates…" escape hatch.
	// When all templates are featured (len(rest) == 0) or none are
	// (len(featured) == 0), skip the curated step and show the full list
	// directly — a curated list that equals the full list adds no value.
	if len(featured) > 0 && len(rest) > 0 {
		defaultIdx := findRecommendedIndex(featured)

		selected, err := promptSelectTemplate(
			ctx, azdClient, featured,
			"Select a starter template", &defaultIdx, true,
		)
		if err != nil {
			return nil, err
		}

		if selected != nil {
			return selected, nil
		}
		// User chose "See all templates…" → fall through to full list.
	}

	// Show the complete catalog (featured + rest, sorted alphabetically).
	allSorted := slices.Clone(langFiltered)
	slices.SortFunc(allSorted, func(a, b AgentTemplate) int {
		return strings.Compare(a.Title, b.Title)
	})

	// Pre-select the recommended template in the full list too.
	recommendedIdx := findRecommendedIndex(allSorted)

	return promptSelectTemplate(
		ctx, azdClient, allSorted,
		"Select an agent template", &recommendedIdx, false,
	)
}

// partitionFeatured splits templates into featured (tagged "featured") and
// the rest. Both slices are sorted alphabetically by title.
func partitionFeatured(templates []AgentTemplate) (featured, rest []AgentTemplate) {
	for _, t := range templates {
		if t.isFeatured() {
			featured = append(featured, t)
		} else {
			rest = append(rest, t)
		}
	}

	sortByTitle := func(a, b AgentTemplate) int {
		return strings.Compare(a.Title, b.Title)
	}
	slices.SortFunc(featured, sortByTitle)
	slices.SortFunc(rest, sortByTitle)

	return featured, rest
}

// findRecommendedIndex returns the index of the recommended default template
// in the given list. It looks for a template tagged "recommended"; if none
// is found it returns 0 (first item in the list).
func findRecommendedIndex(templates []AgentTemplate) int32 {
	for i, t := range templates {
		if t.isRecommended() {
			return boundedInt32Index(i)
		}
	}
	return 0
}

// promptSelectTemplate presents a select prompt for the given templates.
// defaultIdx, when non-nil, pre-selects that index in the list.
// When includeSeeAll is true, a "See all templates…" option is appended;
// selecting it causes the function to return (nil, nil) so the caller can
// re-prompt with the full list.
func promptSelectTemplate(
	ctx context.Context,
	azdClient *azdext.AzdClient,
	templates []AgentTemplate,
	message string,
	defaultIdx *int32,
	includeSeeAll bool,
) (*AgentTemplate, error) {
	choices := make([]*azdext.SelectChoice, len(templates))
	for i, t := range templates {
		choices[i] = &azdext.SelectChoice{
			Label: t.Title,
			Value: fmt.Sprintf("%d", i),
		}
	}

	if includeSeeAll {
		choices = append(choices, &azdext.SelectChoice{
			Label: "See all templates...",
			Value: seeAllSentinel,
		})
	}

	opts := &azdext.SelectOptions{
		Message: message,
		Choices: choices,
	}
	if defaultIdx != nil {
		opts.SelectedIndex = defaultIdx
	}

	resp, err := azdClient.Prompt().Select(ctx, &azdext.SelectRequest{
		Options: opts,
	})
	if err != nil {
		if exterrors.IsCancellation(err) {
			return nil, exterrors.Cancelled("template selection was cancelled")
		}
		return nil, fmt.Errorf("failed to prompt for template: %w", err)
	}

	selected := choices[*resp.Value]
	if selected.Value == seeAllSentinel {
		return nil, nil
	}

	return &templates[*resp.Value], nil
}

// findAgentManifest searches the directory tree rooted at dir for the first
// agent.yaml or agent.manifest.yaml file. Returns the path if found, or empty string if not.
func findAgentManifest(dir string) (string, error) {
	manifestNames := map[string]bool{
		"agent.yaml":          true,
		"agent.manifest.yaml": true,
	}

	var found string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip directories we can't read
		}
		if d.IsDir() {
			return nil
		}
		if manifestNames[strings.ToLower(d.Name())] {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("searching for agent manifest: %w", err)
	}

	return found, nil
}

// detectLocalManifest checks only the immediate directory for an agent manifest file.
// Returns the path to the found manifest (preferring agent.manifest.yaml over agent.yaml,
// then .yml variants), or an empty string if none contain valid manifest content.
// Returns a non-nil error for unexpected I/O failures (e.g. permission errors).
func detectLocalManifest(dir string) (string, error) {
	candidates := []string{
		"agent.manifest.yaml",
		"agent.yaml",
		"agent.manifest.yml",
		"agent.yml",
	}

	for _, name := range candidates {
		candidate := filepath.Join(dir, name)
		_, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("checking for manifest %s: %w", candidate, err)
		}
		if isValidManifestFile(candidate) {
			return candidate, nil
		}
	}
	return "", nil
}

// isValidManifestFile reads the file and checks whether it can be loaded as
// a valid AgentManifest via LoadAndValidateAgentManifest.
func isValidManifestFile(path string) bool {
	//nolint:gosec // path comes from a known filename in a user-controlled directory
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	_, err = agent_yaml.LoadAndValidateAgentManifest(content)
	return err == nil
}
