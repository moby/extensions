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

// TestBinariesRefusesNonRegularFiles verifies that an extension binary that is
// not a regular file fails discovery, even when a symlink's target would pass
// every other check.
func TestBinariesRefusesNonRegularFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	assert.NilError(t, os.WriteFile(target, []byte("x"), 0o755))

	for _, tc := range []struct {
		name   string
		create func(path string) error
	}{
		{name: "symlink", create: func(path string) error { return os.Symlink(target, path) }},
		{name: "fifo", create: func(path string) error {
			if err := syscall.Mkfifo(path, 0o755); err != nil {
				return err
			}
			// Mkfifo applies the umask; make sure the pipe looks executable.
			return os.Chmod(path, 0o755)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "org.example.special.v1")
			assert.NilError(t, tc.create(bin))

			_, err := Binaries(t.Context(), filepath.Dir(bin))
			assert.Error(t, err, `extension binary "`+bin+`" is not a regular file`)
		})
	}
}
