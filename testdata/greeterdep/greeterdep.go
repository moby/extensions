// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

// Package greeterdep is the out-of-process dependency-test fixture.
package greeterdep

import (
	"context"
	"fmt"

	"github.com/moby/extensions"
	greeterv0 "github.com/moby/extensions/example/greeter/v0"
)

// ID is the extension id and binary name.
const ID = "org.mobyproject.example.greeterdep.v1"

// resolver keeps the Init resolver for Shutdown; the fixture process serves
// only this extension.
var resolver extensions.Resolver

// greet calls the greeter dependency and verifies the reply.
func greet(ctx context.Context, r extensions.Resolver) error {
	reply, err := greeterv0.Greet(ctx, r, &greeterv0.HelloRequest{Name: "dep"})
	if err != nil {
		return fmt.Errorf("greeterdep: call greeter dependency: %w", err)
	}
	if reply.Message != "hello dep" {
		return fmt.Errorf("greeterdep: unexpected greeter reply %q", reply.Message)
	}
	return nil
}

// Extension declares a dependency on the greeter point and calls it from both
// Init and Shutdown.
var Extension = extensions.New(extensions.Declaration{
	ID:           ID,
	Dependencies: []extensions.Dependency{greeterv0.Point.Dependency()},
	Init: func(ctx context.Context, _ extensions.Config, r extensions.Resolver) error {
		resolver = r
		return greet(ctx, r)
	},
	Shutdown: func(ctx context.Context) error {
		return greet(ctx, resolver)
	},
})
