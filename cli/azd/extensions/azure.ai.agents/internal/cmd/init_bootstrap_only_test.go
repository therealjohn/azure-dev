// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAzureYaml writes a bootstrap stub via the same renderer the
// pre-flow uses, so the detector and the writer always agree on the
// on-disk shape.
func stubAzureYaml(t *testing.T, dir string) {
	t.Helper()
	body := renderBootstrapAzureYaml("test-project", "1.2.3")
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, bootstrapAzureYamlName), []byte(body), 0o644))
}

// TestDirIsAgentBootstrapOnly_BareStub is the minimum positive case:
// the only file in the dir is the marker-bearing azure.yaml.
func TestDirIsAgentBootstrapOnly_BareStub(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.True(t, ok, "stub-only directory must be bootstrap-only")
}

// TestDirIsAgentBootstrapOnly_StubPlusHousekeeping covers the common
// case: the user ran git init and the pre-flow.
func TestDirIsAgentBootstrapOnly_StubPlusHousekeeping(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My agent\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".vscode"), 0o755))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.True(t, ok, "stub + .git + .gitignore + README.md + .vscode/ should still be bootstrap-only")
}

// TestDirIsAgentBootstrapOnly_StubPlusKnownSkillRoot exercises the
// default install paths (.claude/skills/azd-ai-skill, .agents/skills/...).
// These root names are whitelisted unconditionally so we don't have to
// crack open the skill dir.
func TestDirIsAgentBootstrapOnly_StubPlusKnownSkillRoot(t *testing.T) {
	for _, root := range []string{".claude", ".agents"} {
		t.Run(root, func(t *testing.T) {
			dir := t.TempDir()
			stubAzureYaml(t, dir)
			require.NoError(t, os.MkdirAll(filepath.Join(dir, root, "skills", "azd-ai-skill"), 0o755))
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, root, "skills", "azd-ai-skill", "SKILL.md"),
				[]byte("---\nname: AZD AI\n---\n"), 0o644))

			ok, err := dirIsAgentBootstrapOnly(dir)
			require.NoError(t, err)
			assert.True(t, ok)
		})
	}
}

// TestDirIsAgentBootstrapOnly_GitFileWorktreePointer pins the
// rubber-duck #6 finding: in git worktrees, .git is a FILE pointing to
// the real gitdir, not a directory. Both shapes must classify as
// whitelisted housekeeping.
func TestDirIsAgentBootstrapOnly_GitFileWorktreePointer(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"),
		[]byte("gitdir: /tmp/somewhere/.git/worktrees/x\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.True(t, ok, ".git as a file (worktree pointer) must be whitelisted just like .git as a dir")
}

// TestDirIsAgentBootstrapOnly_StubPlusCustomSkillDir verifies the
// SKILL.md probe (rubber-duck #3) for custom install paths the user
// chose via the pre-flow's Q3=custom branch.
func TestDirIsAgentBootstrapOnly_StubPlusCustomSkillDir(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	custom := filepath.Join(dir, ".my-tool", "skills", "azd-ai-skill")
	require.NoError(t, os.MkdirAll(custom, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(custom, "SKILL.md"),
		[]byte("---\nname: AZD AI\ndescription: blah\n---\n\nbody\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.True(t, ok, "custom skill path discovered via SKILL.md probe should count as bootstrap noise")
}

// TestDirIsAgentBootstrapOnly_StrayPyFile is the headline negative case:
// the user added agent code, so we MUST NOT silently route them through
// the from-code path.
func TestDirIsAgentBootstrapOnly_StrayPyFile(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hi')\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok, "stray .py file means user code is present; bootstrap-only must be false")
}

// TestDirIsAgentBootstrapOnly_StrayPackageJson covers the JavaScript /
// TypeScript equivalent of the Python negative case.
func TestDirIsAgentBootstrapOnly_StrayPackageJson(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestDirIsAgentBootstrapOnly_AzureYamlWithoutMarker pins the
// load-bearing constraint: the marker is REQUIRED. A user's existing
// azure.yaml from a real template MUST NOT be misclassified as
// bootstrap.
func TestDirIsAgentBootstrapOnly_AzureYamlWithoutMarker(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "azure.yaml"),
		[]byte("name: real-project\nmetadata:\n  template: todo-csharp@1.0\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok, "azure.yaml without bootstrap marker MUST NOT classify as bootstrap-only")
}

// TestDirIsAgentBootstrapOnly_AzureYamlWithMarkerButServices is the
// rubber-duck #5 case: once addToProject populates services:, the
// stub marker is meaningless and the dir is a real project.
func TestDirIsAgentBootstrapOnly_AzureYamlWithMarkerButServices(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "azure.yaml"),
		[]byte("name: x\nmetadata:\n  template: azd-ai-bootstrap@1.2.3\nservices:\n  api:\n    host: appservice\n"),
		0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok, "marker + services: => real project, not bootstrap-only")
}

// TestDirIsAgentBootstrapOnly_AzureYamlWithMarkerButHooks confirms the
// no-hooks half of the no-services-or-infra-or-hooks constraint.
func TestDirIsAgentBootstrapOnly_AzureYamlWithMarkerButHooks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "azure.yaml"),
		[]byte("name: x\nmetadata:\n  template: azd-ai-bootstrap@1.2.3\nhooks:\n  predeploy:\n    posix: {run: echo}\n"),
		0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestDirIsAgentBootstrapOnly_UnknownTopLevelDir covers the case where
// the user has a real source directory at the top level. We must NOT
// fall through into it just because it's a directory.
func TestDirIsAgentBootstrapOnly_UnknownTopLevelDir(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "main.py"), []byte("x = 1\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok, "unknown src/ dir with user code must not classify as bootstrap-only")
}

// TestDirIsAgentBootstrapOnly_EmptyDirReturnsFalse documents the
// contract: bootstrap-only REQUIRES the marker, and an empty dir has
// no marker. Callers route empty dirs via dirIsEmpty first.
func TestDirIsAgentBootstrapOnly_EmptyDirReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestDirIsAgentBootstrapOnly_CaseInsensitiveWhitelist exercises the
// case-insensitive matching for housekeeping files on case-insensitive
// filesystems (Windows / default macOS APFS).
func TestDirIsAgentBootstrapOnly_CaseInsensitiveWhitelist(t *testing.T) {
	dir := t.TempDir()
	stubAzureYaml(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Readme.MD"), []byte("# r\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("mit\n"), 0o644))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestDirIsAgentBootstrapOnly_PropagatesReadDirErrors confirms we do
// NOT swallow I/O errors (rubber-duck #7). On non-Windows we make the
// dir unreadable so os.ReadDir fails with EACCES.
func TestDirIsAgentBootstrapOnly_PropagatesReadDirErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based unreadable directory trick is not portable on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := dirIsAgentBootstrapOnly(dir)
	require.Error(t, err, "EACCES on the target dir must be propagated, not coerced to false")
}

// TestDirIsAgentBootstrapOnly_SymlinkOutsideRootRejected pins the
// symlink-safety rule (rubber-duck #6). A symlinked .github/ pointing
// outside the project root should NOT classify as whitelisted.
func TestDirIsAgentBootstrapOnly_SymlinkOutsideRootRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink on Windows generally requires developer mode or admin")
	}
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))

	dir := filepath.Join(parent, "project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stubAzureYaml(t, dir)
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, ".github")))

	ok, err := dirIsAgentBootstrapOnly(dir)
	require.NoError(t, err)
	assert.False(t, ok, "symlinked .github/ pointing outside the project must reject the dir")
}
