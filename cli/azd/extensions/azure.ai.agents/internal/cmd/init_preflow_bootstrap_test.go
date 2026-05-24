// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestRenderBootstrapAzureYaml_HasMarkerAndSchema pins the on-disk
// shape of the stub. dirIsAgentBootstrapOnly relies on the
// `metadata.template: azd-ai-bootstrap@*` marker; the schema comment
// keeps editor tooling happy.
func TestRenderBootstrapAzureYaml_HasMarkerAndSchema(t *testing.T) {
	got := renderBootstrapAzureYaml("my-agent-project", "1.2.3")
	assert.Contains(t, got, "schemas/v1.0/azure.yaml.json",
		"stub should include the azure.yaml schema URL comment")
	assert.Contains(t, got, "name: my-agent-project",
		"stub should declare the project name")
	assert.Contains(t, got, "metadata:")
	assert.Contains(t, got, "template: azd-ai-bootstrap@1.2.3",
		"stub MUST carry the bootstrap marker dirIsAgentBootstrapOnly looks for")
}

// TestRenderBootstrapAzureYaml_ParsesAsValidYAML guards against
// future format edits that would produce something the YAML parser
// (and therefore azd core's project loader) cannot read.
func TestRenderBootstrapAzureYaml_ParsesAsValidYAML(t *testing.T) {
	body := renderBootstrapAzureYaml("ok", "0.1.0-beta.1")
	var doc struct {
		Name     string `yaml:"name"`
		Metadata struct {
			Template string `yaml:"template"`
		} `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(body), &doc))
	assert.Equal(t, "ok", doc.Name)
	assert.Equal(t, "azd-ai-bootstrap@0.1.0-beta.1", doc.Metadata.Template)
}

// TestRenderBootstrapAzureYaml_FallsBackWhenVersionEmpty defends
// against an unexpectedly empty version.Version (a misbuilt binary)
// so we never emit a syntactically-broken "azd-ai-bootstrap@" value
// that the detection helper would then fail to parse.
func TestRenderBootstrapAzureYaml_FallsBackWhenVersionEmpty(t *testing.T) {
	got := renderBootstrapAzureYaml("ok", "")
	assert.Contains(t, got, "template: azd-ai-bootstrap@dev",
		"empty version should fall back to dev to keep the marker syntactically valid")
}

// TestWriteBootstrapAzureYaml_WritesWhenAbsent is the happy path:
// fresh directory => stub appears with the marker.
func TestWriteBootstrapAzureYaml_WritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, writeBootstrapAzureYaml(dir))

	body, err := os.ReadFile(filepath.Join(dir, bootstrapAzureYamlName))
	require.NoError(t, err)
	assert.Contains(t, string(body), bootstrapTemplatePrefix+"@",
		"written file should carry the bootstrap marker")
	assert.Contains(t, string(body), "name: "+sanitizeAgentName(filepath.Base(dir)),
		"written file should use the sanitized cwd basename for name:")
}

// TestWriteBootstrapAzureYaml_IsNoopWhenFileExists pins the O_EXCL
// contract: an existing azure.yaml MUST NOT be touched, since it could
// be a real azd project the user has invested in. The function should
// return nil (success no-op) so the pre-flow continues normally.
func TestWriteBootstrapAzureYaml_IsNoopWhenFileExists(t *testing.T) {
	dir := t.TempDir()
	preexisting := "name: existing-project\n# user owned\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, bootstrapAzureYamlName),
		[]byte(preexisting), 0o644))

	require.NoError(t, writeBootstrapAzureYaml(dir),
		"O_EXCL collision must be treated as success no-op")

	got, err := os.ReadFile(filepath.Join(dir, bootstrapAzureYamlName))
	require.NoError(t, err)
	assert.Equal(t, preexisting, string(got),
		"pre-existing azure.yaml MUST remain byte-for-byte unchanged")
}

// TestWriteBootstrapAzureYaml_SanitizesUglyDirName covers basenames
// with characters that aren't valid in `name:` (spaces, dots, mixed
// case). sanitizeAgentName already has its own dedicated tests; here
// we just confirm we are routing the basename through it.
func TestWriteBootstrapAzureYaml_SanitizesUglyDirName(t *testing.T) {
	parent := t.TempDir()
	ugly := filepath.Join(parent, "My Cool.AI Agent")
	require.NoError(t, os.MkdirAll(ugly, 0o755))

	require.NoError(t, writeBootstrapAzureYaml(ugly))

	body, err := os.ReadFile(filepath.Join(ugly, bootstrapAzureYamlName))
	require.NoError(t, err)
	// sanitizeAgentName lowercases and replaces non-[a-z0-9-] with '-'.
	assert.Contains(t, string(body), "name: my-cool-ai-agent",
		"basename should be passed through sanitizeAgentName")
}

// TestWriteBootstrapAzureYaml_PropagatesNonExistErrors confirms we do
// NOT swallow real write failures. The pre-flow treats these as fatal;
// silently ignoring them would let the user paste the starter prompt
// into their coding agent only to have the follow-up `azd ai agent
// init` fail on the unfixed "use existing code vs template" gap.
//
// On Windows, chmod is largely a no-op so a read-only dir trick does
// not reliably block the write; skip there. On macOS / Linux a 0o500
// dir denies create, which is enough.
func TestWriteBootstrapAzureYaml_PropagatesNonExistErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory permissions are not enforced for OpenFile on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := writeBootstrapAzureYaml(dir)
	require.Error(t, err, "should surface permission-denied as a real error")
	assert.True(t,
		strings.Contains(err.Error(), bootstrapAzureYamlName),
		"error should name the file we failed to write; got %v", err)
}
