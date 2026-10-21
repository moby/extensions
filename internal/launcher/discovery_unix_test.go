// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package launcher

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

// TestBinariesRefusesNonRegularFiles verifies that only regular files are
// launched, even when a symlink's target would pass every other check.
func TestBinariesRefusesNonRegularFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	assert.NilError(t, os.WriteFile(target, []byte("x"), 0o755))

	dir := t.TempDir()
	good := filepath.Join(dir, "org.example.good.v1")
	assert.NilError(t, os.WriteFile(good, []byte("x"), 0o755))
	assert.NilError(t, os.Symlink(target, filepath.Join(dir, "org.example.link.v1")))
	fifo := filepath.Join(dir, "org.example.fifo.v1")
	assert.NilError(t, syscall.Mkfifo(fifo, 0o755))
	// Mkfifo applies the umask; make sure the pipe looks executable.
	assert.NilError(t, os.Chmod(fifo, 0o755))

	bins, err := Binaries(t.Context(), dir)
	assert.NilError(t, err)
	assert.DeepEqual(t, bins, []string{good})
}
