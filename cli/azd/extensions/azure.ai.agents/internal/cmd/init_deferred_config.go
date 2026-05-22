// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/azure/azure-dev/cli/azd/pkg/output"
)

// deferredConfigKind identifies a single piece of Azure configuration that
// could not be resolved during a `--no-prompt` run of `azd ai agent init`.
//
// Each kind maps to a stable next-steps line in the consolidated warning
// block that is printed after init successfully writes azure.yaml. Storing
// kinds (rather than pre-formatted strings) lets the emitter dedupe entries
// and keep wording in one place.
type deferredConfigKind int

const (
	deferredSubscriptionID deferredConfigKind = iota
	deferredLocation
	deferredFoundryProject
	// deferredModelVersion is recorded when init produced a deployments[]
	// entry but the model `version` could not be filled in (typically because
	// subscription/location were deferred, so the Azure model catalog could
	// not be queried). The next-steps block tells the user to backfill the
	// version with `azd ai agent model set <model> --version <v>` before
	// running `azd provision`.
	deferredModelVersion
)

// deferredAzureConfig tracks which Azure-context items were skipped during a
// `--no-prompt` run because they would have required interactive input.
//
// Items added via Add are deduped so the same line never appears twice in
// the next-steps warning block, even if multiple branches in
// configureModelChoice / createDefinitionFromLocalAgent flag the same gap.
type deferredAzureConfig struct {
	items []deferredConfigKind
}

// Add records that the given configuration item was deferred. Calling Add
// multiple times with the same kind is safe and has no additional effect.
func (d *deferredAzureConfig) Add(kind deferredConfigKind) {
	if d == nil {
		return
	}
	if slices.Contains(d.items, kind) {
		return
	}
	d.items = append(d.items, kind)
}

// IsEmpty reports whether any deferred items have been recorded.
func (d *deferredAzureConfig) IsEmpty() bool {
	return d == nil || len(d.items) == 0
}

// Emit prints the consolidated next-steps warning block to stdout. It is a
// no-op when no items were deferred, so callers can invoke it unconditionally
// at the end of init.
//
// The block is written using output.WithWarningFormat and is ASCII-only so
// it renders consistently in terminals and when copy-pasted into issues.
func (d *deferredAzureConfig) Emit() {
	d.EmitTo(os.Stdout)
}

// EmitTo writes the consolidated next-steps warning block to the provided
// writer. Production callers use Emit (which writes to os.Stdout); tests use
// EmitTo with a bytes.Buffer so they can run in parallel without fighting
// over the global os.Stdout.
func (d *deferredAzureConfig) EmitTo(w io.Writer) {
	if d.IsEmpty() {
		return
	}

	var b strings.Builder
	b.WriteString("\nInit completed with deferred Azure configuration:\n\n")
	for _, kind := range d.items {
		switch kind {
		case deferredSubscriptionID:
			b.WriteString("  * AZURE_SUBSCRIPTION_ID is not set. " +
				"Run: azd env set AZURE_SUBSCRIPTION_ID <subscription-id>\n")
		case deferredLocation:
			b.WriteString("  * AZURE_LOCATION is not set. " +
				"Run: azd env set AZURE_LOCATION <region>\n")
		case deferredFoundryProject:
			b.WriteString("  * Foundry project selection was skipped. " +
				"A new Foundry project will be created on `azd up`. " +
				"To target an existing project, run `azd env set AZURE_AI_PROJECT_ID <project-id>` " +
				"to set AZURE_AI_PROJECT_ID in the azd environment.\n")
		case deferredModelVersion:
			b.WriteString("  * Model deployment version could not be resolved without subscription/location. " +
				"Run `azd ai agent model set <model> --agent-name <agent> --version <version>` " +
				"after setting AZURE_SUBSCRIPTION_ID and AZURE_LOCATION " +
				"(or pass --version explicitly) before `azd provision`.\n")
		}
	}
	b.WriteString("\nSet the values above before running `azd provision` or `azd up`.\n")

	fmt.Fprint(w, output.WithWarningFormat("%s", b.String()))
}
