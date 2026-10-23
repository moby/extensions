// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestValidateExtensionConfigKey(t *testing.T) {
	valid := []ExtensionID{
		"org.example.myext.v1",
		"org.example.myext",
		"org.example.s3-volume",
		"org.example.v1",
	}
	for _, key := range valid {
		if err := ValidateExtensionConfigKey(key); err != nil {
			t.Errorf("ValidateExtensionConfigKey(%q) = %v, want nil", key, err)
		}
	}

	invalid := []ExtensionID{
		"",
		"foo",                     // only one name segment
		"org.example.myext.v*",    // wildcard
		"org.example.myext.*",     // wildcard
		"org.example.myext.v1.v2", // name ends in a version
		"Org.Example.myext",       // uppercase
		"org.example/evil",        // path separator
	}
	for _, key := range invalid {
		if err := ValidateExtensionConfigKey(key); err == nil {
			t.Errorf("ValidateExtensionConfigKey(%q) = nil, want error", key)
		}
	}
}

func TestLookupConfig(t *testing.T) {
	exact := Config{"source": "exact"}
	anyVersion := Config{"source": "any version"}
	configs := map[ExtensionID]Config{
		"org.example.foo.v1": exact,
		"org.example.foo":    anyVersion,
		"org.example.bar.v1": {"source": "bar"},
	}

	for _, tc := range []struct {
		name string
		id   ExtensionID
		want Config
	}{
		{name: "exact id takes precedence", id: "org.example.foo.v1", want: exact},
		{name: "name key covers other versions", id: "org.example.foo.v2", want: anyVersion},
		{name: "other extension without a key", id: "org.example.baz.v1", want: nil},
		{name: "exact key does not cover other versions", id: "org.example.bar.v2", want: nil},
		{name: "name prefix does not match", id: "org.example.foobar.v1", want: nil},
		{name: "name key does not cover nested names", id: "org.example.foo.runtime.v1", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.DeepEqual(t, LookupConfig(configs, tc.id), tc.want)
		})
	}
}
