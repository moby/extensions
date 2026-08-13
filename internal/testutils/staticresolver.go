// Package testutils provides test doubles for the extension framework.
package testutils

import (
	"fmt"

	"github.com/moby/extensions"
)

// StaticResolver answers from a fixed set of point providers.
type StaticResolver []StaticProvider

// StaticProvider is one provider entry in a StaticResolver.
type StaticProvider struct {
	Point     extensions.PointID
	Extension extensions.ExtensionID
	Impl      any
	Builtin   bool
}

// Provide returns a static provider entry for point.
func Provide[T any](point extensions.Point[T], impl any) StaticProvider {
	return StaticProvider{Point: point.ID(), Impl: impl}
}

// Provider implements [extensions.Resolver].
func (r StaticResolver) Provider(point extensions.PointID, id extensions.ExtensionID) (any, error) {
	for _, p := range r.Providers(point) {
		if p.Extension == id {
			return p.Impl, nil
		}
	}
	return nil, fmt.Errorf("no provider for point %q from extension %q", point, id)
}

// Providers implements [extensions.Resolver].
func (r StaticResolver) Providers(point extensions.PointID) []extensions.ResolvedProvider {
	var providers []extensions.ResolvedProvider
	for _, provider := range r {
		if provider.Point != point {
			continue
		}
		id := provider.Extension
		if id == "" {
			id = extensions.ExtensionID(fmt.Sprintf("org.example.test%d.v1", len(providers)+1))
		}
		providers = append(providers, extensions.ResolvedProvider{
			Extension: id,
			Impl:      provider.Impl,
			Builtin:   provider.Builtin,
		})
	}
	return providers
}
