// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestGeneratedStreamingRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests generated packages")
	}
	t.Parallel()
	root := repositoryRoot(t)
	// Use a directory inside the module because this package has no standalone
	// go.mod; this resolves the current checkout's vendor tree. Under testdata,
	// repository-wide ./... patterns skip it even if cleanup does not run.
	dir, err := os.MkdirTemp(filepath.Join(root, "cmd", "mobyextgen", "testdata"), "runtime-") //nolint:usetesting // t.TempDir is outside the module.
	assert.NilError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	writeFixture(t, filepath.Join(dir, "contract.go"), "runtime_contract.go.txt")
	importPath, _, err := locate(dir)
	assert.NilError(t, err)
	behavior := strings.NewReplacer(
		"__CONTRACT_IMPORT__", importPath,
		"__PROTOGEN_IMPORT__", path.Join(importPath, "protogen"),
	).Replace(readFixture(t, "runtime_behavior_test.go.txt"))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "runtime_test.go"), []byte(behavior), 0o644))
	transport := strings.NewReplacer(
		"__CONTRACT_IMPORT__", importPath,
		"__PROTOGEN_IMPORT__", path.Join(importPath, "protogen"),
	).Replace(readFixture(t, "runtime_transport_test.go.txt"))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "runtime_transport_test.go"), []byte(transport), 0o644))
	assert.NilError(t, run(dir, ""))

	for _, fixture := range []struct {
		name   string
		source string
		id     string
	}{
		{
			name:   "unary",
			source: "runtime_unary_contract.go.txt",
			id:     "org.example.generated.unary.v1.Service",
		},
		{
			name:   "serverstream",
			source: "runtime_server_contract.go.txt",
			id:     "org.example.generated.serverstream.v1.Service",
		},
		{
			name:   "clientstream",
			source: "runtime_client_contract.go.txt",
			id:     "org.example.generated.clientstream.v1.Service",
		},
	} {
		subdir := filepath.Join(dir, fixture.name)
		assert.NilError(t, os.MkdirAll(subdir, 0o755))
		writeFixture(t, filepath.Join(subdir, "contract.go"), fixture.source)
		assert.NilError(t, run(subdir, fixture.id))
	}

	args := []string{"test"}
	if _, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); err == nil {
		args = append(args, "-mod=vendor")
	}
	if runtimeRace {
		args = append(args, "-race")
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated runtime test failed: %v\n%s", err, output)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(".")
	assert.NilError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("go.mod not found above generator package")
		}
		root = parent
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	assert.NilError(t, err)
	return string(content)
}

func writeFixture(t *testing.T, destination, name string) {
	t.Helper()
	assert.NilError(t, os.WriteFile(destination, []byte(readFixture(t, name)), 0o644))
}
