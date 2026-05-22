// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"azureaiagent/internal/exterrors"

	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/stretchr/testify/require"
)

// TestTryEnsureSubscription_DefersWhenNoPromptAndMissing verifies that the
// helper short-circuits without calling out to the host when the subscription
// is not already known and we are in --no-prompt mode.
func TestTryEnsureSubscription_DefersWhenNoPromptAndMissing(t *testing.T) {
	t.Parallel()

	azureContext := &azdext.AzureContext{
		Scope: &azdext.AzureScope{},
	}

	// No azdClient is needed: the helper must return before issuing any RPC.
	cred, deferred, err := tryEnsureSubscription(
		t.Context(), nil, azureContext, "envName",
		"prompt message", true,
	)

	require.NoError(t, err)
	require.True(t, deferred)
	require.Nil(t, cred)
}

// TestTryEnsureLocation_DefersWhenNoPromptAndMissing verifies that the helper
// short-circuits without contacting the host when the location is unset and
// we are in --no-prompt mode.
func TestTryEnsureLocation_DefersWhenNoPromptAndMissing(t *testing.T) {
	t.Parallel()

	azureContext := &azdext.AzureContext{
		Scope: &azdext.AzureScope{},
	}

	deferred, err := tryEnsureLocation(t.Context(), nil, azureContext, "envName", true)

	require.NoError(t, err)
	require.True(t, deferred)
}

// TestTryEnsureLocation_UnsupportedExistingLocationFailsLoudly verifies that
// an explicitly-set but unsupported AZURE_LOCATION returns a structured
// validation error in --no-prompt mode, rather than silently deferring.
// This protects the user from typos (e.g. "westus99") that would otherwise be
// swept under the rug and surface as opaque provisioning failures later.
func TestTryEnsureLocation_UnsupportedExistingLocationFailsLoudly(t *testing.T) {
	// Not Parallel: this test mutates the package-level regions cache.
	seedRegionsCache(t, []string{"eastus", "westus2"})

	azureContext := &azdext.AzureContext{
		Scope: &azdext.AzureScope{
			Location: "westus99",
		},
	}

	deferred, err := tryEnsureLocation(t.Context(), nil, azureContext, "envName", true)

	require.False(t, deferred)
	require.Error(t, err)
	require.Contains(t, err.Error(), "westus99")
	require.Contains(t, err.Error(), "is not a supported region")
}

// TestTryEnsureSubscriptionAndLocation_BothDeferred verifies the combined
// helper surfaces independent deferral flags so callers can include only the
// missing values in the next-steps warning.
func TestTryEnsureSubscriptionAndLocation_BothDeferred(t *testing.T) {
	t.Parallel()

	azureContext := &azdext.AzureContext{
		Scope: &azdext.AzureScope{},
	}

	cred, subDeferred, locDeferred, err := tryEnsureSubscriptionAndLocation(
		t.Context(), nil, azureContext, "envName",
		"prompt message", true,
	)

	require.NoError(t, err)
	require.True(t, subDeferred)
	require.True(t, locDeferred)
	require.Nil(t, cred)
}

// TestDeferredAzureConfig_AddDedupes verifies that repeated Add calls for the
// same kind result in a single entry, so the next-steps block never repeats
// itself when multiple branches in the init flow flag the same gap.
func TestDeferredAzureConfig_AddDedupes(t *testing.T) {
	t.Parallel()

	d := deferredAzureConfig{}
	d.Add(deferredSubscriptionID)
	d.Add(deferredSubscriptionID)
	d.Add(deferredLocation)
	d.Add(deferredLocation)
	d.Add(deferredSubscriptionID)

	require.False(t, d.IsEmpty())
	require.Equal(t,
		[]deferredConfigKind{deferredSubscriptionID, deferredLocation},
		d.items,
	)
}

// TestDeferredAzureConfig_EmitNoOpWhenEmpty verifies the consolidated next-
// steps block is omitted entirely when nothing was deferred, so headless
// callers in fully-configured environments see clean output.
func TestDeferredAzureConfig_EmitNoOpWhenEmpty(t *testing.T) {
	t.Parallel()

	d := deferredAzureConfig{}

	var buf bytes.Buffer
	d.EmitTo(&buf)

	require.Empty(t, buf.String())
}

// TestDeferredAzureConfig_EmitWritesEachKindOnce verifies that every kind
// appears in the printed block, that the block mentions the env var names
// and `azd env set` recipes, and that nothing is repeated.
func TestDeferredAzureConfig_EmitWritesEachKindOnce(t *testing.T) {
	t.Parallel()

	d := deferredAzureConfig{}
	d.Add(deferredSubscriptionID)
	d.Add(deferredLocation)
	d.Add(deferredFoundryProject)
	d.Add(deferredModelVersion)

	var buf bytes.Buffer
	d.EmitTo(&buf)
	out := buf.String()

	require.Contains(t, out, "Init completed with deferred Azure configuration")
	require.Contains(t, out, "AZURE_SUBSCRIPTION_ID is not set")
	require.Contains(t, out, "azd env set AZURE_SUBSCRIPTION_ID")
	require.Contains(t, out, "AZURE_LOCATION is not set")
	require.Contains(t, out, "azd env set AZURE_LOCATION")
	require.Contains(t, out, "Foundry project selection was skipped")
	require.Contains(t, out, "azd ai agent model set")
	require.Contains(t, out, "--version")

	// Each marker appears exactly once.
	require.Equal(t, 1, strings.Count(out, "AZURE_SUBSCRIPTION_ID is not set"))
	require.Equal(t, 1, strings.Count(out, "AZURE_LOCATION is not set"))
	require.Equal(t, 1, strings.Count(out, "azd ai agent model set"))
}

// seedRegionsCache primes the package-level regions cache so location-aware
// helpers don't need network access during tests. The cache is restored to
// its original value at test end so other tests are unaffected.
func seedRegionsCache(t *testing.T, regions []string) {
	t.Helper()

	regionsCache.mu.Lock()
	originalRegions := regionsCache.regions
	originalInflight := regionsCache.inflight
	regionsCache.regions = regions
	regionsCache.inflight = nil
	regionsCache.mu.Unlock()

	t.Cleanup(func() {
		regionsCache.mu.Lock()
		regionsCache.regions = originalRegions
		regionsCache.inflight = originalInflight
		regionsCache.mu.Unlock()
	})
}

// Compile-time check that exterrors.IsPromptRequired keeps the signature the
// helpers rely on. Failing this build is preferable to silently dropping the
// no-prompt detection path.
var _ = exterrors.IsPromptRequired
