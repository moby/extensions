// Package testutils provides test doubles for the extension framework.
package testutils

import (
	"fmt"

	"github.com/moby/extensions"
)

// StaticResolver answers from a fixed provider set for point tests.
type StaticResolver []extensions.ResolvedProvider

// Provide returns a StaticResolver serving impls in order under generated ids.
func Provide(impls ...any) StaticResolver {
	r := make(StaticResolver, 0, len(impls))
	for i, impl := range impls {
		r = append(r, extensions.ResolvedProvider{
			Extension: extensions.ExtensionID(fmt.Sprintf("org.example.test%d.v1", i+1)),
			Impl:      impl,
		})
	}
	return r
}

// Provider implements [extensions.Resolver].
func (r StaticResolver) Provider(_ extensions.PointID, id extensions.ExtensionID) (any, error) {
	for _, p := range r {
		if p.Extension == id {
			return p.Impl, nil
		}
	}
	return nil, fmt.Errorf("no provider for extension %q", id)
}

// Providers implements [extensions.Resolver].
func (r StaticResolver) Providers(extensions.PointID) []extensions.ResolvedProvider { return r }
