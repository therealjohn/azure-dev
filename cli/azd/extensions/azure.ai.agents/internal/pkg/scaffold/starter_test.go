// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package scaffold

import (
	"sort"
	"testing"
)

// expectedStarterFiles is the exact list of files baked into the binary by
// the //go:embed directives in resources/resources.go.
//
// This test catches two regressions:
//
//  1. Accidental INCLUSION of generated artifacts (e.g. infra/main.json,
//     the ARM-compiled output of main.bicep) that should never ship in a
//     user's workspace.
//  2. Accidental OMISSION of a module under infra/modules/ -- main.bicep
//     references modules by relative path, so a missing module fails at
//     `azd provision` time rather than at our build time.
//
// When intentionally adding or removing an asset, update this list AND
// resources/starter/ in lock-step.
var expectedStarterFiles = []string{
	"azure.yaml",
	"infra/main.bicep",
	"infra/main.parameters.json",
	"infra/modules/account-cap-host.bicep",
	"infra/modules/acr.bicep",
	"infra/modules/ai-account.bicep",
	"infra/modules/ai-project.bicep",
	"infra/modules/ai-search-knowledge.bicep",
	"infra/modules/ai-search-rbac.bicep",
	"infra/modules/ai-search.bicep",
	"infra/modules/bing-grounding.bicep",
	"infra/modules/connection.bicep",
	"infra/modules/cosmos-rbac-post.bicep",
	"infra/modules/cosmos-rbac-pre.bicep",
	"infra/modules/cosmos.bicep",
	"infra/modules/private-endpoint-and-dns-data.bicep",
	"infra/modules/private-endpoint-and-dns.bicep",
	"infra/modules/project-cap-host.bicep",
	"infra/modules/storage-rbac-standard.bicep",
	"infra/modules/storage-rbac.bicep",
	"infra/modules/storage.bicep",
	"infra/modules/vnet.bicep",
}

func TestStarterManifest_MatchesExpectedFileList(t *testing.T) {
	t.Parallel()

	files, err := StarterManifest()
	if err != nil {
		t.Fatalf("StarterManifest returned error: %v", err)
	}

	got := make([]string, 0, len(files))
	for _, f := range files {
		if len(f.Bytes) == 0 {
			t.Errorf("embedded file %q has zero bytes", f.Path)
		}
		got = append(got, f.Path)
	}
	sort.Strings(got)

	want := make([]string, len(expectedStarterFiles))
	copy(want, expectedStarterFiles)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("embedded file count mismatch: got %d, want %d\n got=%v\nwant=%v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("embedded file mismatch at index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestStarterManifest_NoCRLFInTextAssets guards against contributor
// workstations re-injecting CRLF into the embedded payload via Git
// autocrlf. The .gitattributes in the repo root pins these to LF; this
// test makes the contract enforceable at PR time.
func TestStarterManifest_NoCRLFInTextAssets(t *testing.T) {
	t.Parallel()

	files, err := StarterManifest()
	if err != nil {
		t.Fatalf("StarterManifest returned error: %v", err)
	}

	for _, f := range files {
		for i, b := range f.Bytes {
			if b == '\r' {
				t.Errorf("embedded asset %q contains CR byte at offset %d; expected LF-only line endings (check .gitattributes)",
					f.Path, i)
				break
			}
		}
	}
}
