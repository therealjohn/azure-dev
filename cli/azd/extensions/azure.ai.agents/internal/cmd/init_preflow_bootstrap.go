// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// init_preflow_bootstrap.go owns the bootstrap "azure.yaml" stub the
// agent-driven onboarding pre-flow writes after Q1=Yes. The stub gives
// the directory enough of an azd-project shape that the FOLLOW-UP
// `azd ai agent init [--no-prompt]` invocation -- the one the coding
// agent runs after pasting the starter prompt -- can pick up where the
// pre-flow left off without re-prompting "use existing code vs start
// from a template?".
//
// The stub carries a recognizable marker (metadata.template:
// azd-ai-bootstrap@<version>) so dirIsAgentBootstrapOnly can detect
// that the project was scaffolded by the pre-flow rather than by a
// real `azd init -t <template>` run.

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"azureaiagent/internal/version"
)

// bootstrapTemplatePrefix is the prefix written into `metadata.template`
// to mark a project as having been scaffolded by the pre-flow. The
// `<version>` suffix is appended at write time so older binaries can
// still recognize newer stubs via prefix match.
const bootstrapTemplatePrefix = "azd-ai-bootstrap"

// bootstrapAzureYamlName is the filename written into the project root.
// Hoisted to a constant so the detection helper in
// init_from_templates_helpers.go can reference the exact same name and
// the two cannot drift silently.
const bootstrapAzureYamlName = "azure.yaml"

// writeBootstrapAzureYaml writes the bootstrap stub to <cwd>/azure.yaml
// using O_CREATE|O_EXCL so we NEVER clobber an existing file. The
// no-op-on-EEXIST branch covers three legitimate cases:
//
//  1. The user already had a real azd project here.
//  2. A previous pre-flow run wrote the stub (re-running the pre-flow
//     in the same dir is supported).
//  3. A concurrent process raced us to the write -- ours loses, theirs
//     wins. Either way the file exists and the caller can proceed.
//
// Returns a non-nil error for any OTHER failure (permission denied,
// disk full, unwritable filesystem). The pre-flow treats that error as
// fatal: if we cannot create the stub, the coding agent's follow-up
// invocation will hit the unfixed "use existing code vs template" gap,
// so we must surface the failure BEFORE printing "you're ready to go".
func writeBootstrapAzureYaml(cwd string) error {
	path := filepath.Join(cwd, bootstrapAzureYamlName)

	// Derive a sane name from the directory. sanitizeAgentName falls
	// back to "my-agent" when the basename is unusable, which is fine
	// for a bootstrap stub the user is expected to edit later.
	name := sanitizeAgentName(filepath.Base(cwd))

	content := renderBootstrapAzureYaml(name, version.Version)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// File already exists -- success no-op (case 1/2/3 above).
			return nil
		}
		return fmt.Errorf("create %s: %w", bootstrapAzureYamlName, err)
	}

	// Close errors on the happy path are surfaced via the defer below
	// only if no earlier write error already covered them; failure to
	// flush a 4-line stub matters because it would produce an empty or
	// partial file in the user's project root.
	var writeErr error
	defer func() {
		if cerr := f.Close(); cerr != nil && writeErr == nil {
			writeErr = fmt.Errorf("close %s: %w", bootstrapAzureYamlName, cerr)
		}
	}()

	if _, err := f.Write([]byte(content)); err != nil {
		writeErr = fmt.Errorf("write %s: %w", bootstrapAzureYamlName, err)
		return writeErr
	}
	return writeErr
}

// renderBootstrapAzureYaml builds the stub body. Extracted so tests can
// assert byte-for-byte content without invoking the filesystem and so a
// future caller (e.g. a unit-tested template generator) can reuse it.
//
// The schema comment matches the convention used by the awesome-azd
// templates so editors with the YAML schema extension immediately get
// completions on the file. The metadata.template value is the marker
// dirIsAgentBootstrapOnly looks for.
func renderBootstrapAzureYaml(name, ver string) string {
	if ver == "" {
		// version.Version defaults to "dev" but defend against an
		// unexpectedly empty build flag rather than emit an invalid
		// "azd-ai-bootstrap@" value.
		ver = "dev"
	}
	return fmt.Sprintf(
		"# yaml-language-server: $schema=https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json\n"+
			"\n"+
			"name: %s\n"+
			"metadata:\n"+
			"  template: %s@%s\n",
		name, bootstrapTemplatePrefix, ver)
}
