// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

// Package greeter is the socket-exposure integration-test fixture.
package greeter

import (
	"context"

	"github.com/moby/extensions"
	greeterv0 "github.com/moby/extensions/example/greeter/v0"
	greeterpb "github.com/moby/extensions/example/greeter/v0/protogen"
	servicev0 "github.com/moby/extensions/extpoints/service/v0"
	"github.com/moby/extensions/serverpoint"
)

// ID is the extension id and binary name.
const ID = "org.mobyproject.example.greeter.v1"

// Point deliberately differs from the proto package serving Greeter.
var Point = extensions.DefinePoint[greeterv0.Greeter]("org.example.greeting.v1")

var ServerPoint = serverpoint.Registration{Point: Point.ID(), Register: greeterpb.ServerPoint.Register}

type greeter struct{}

func (greeter) Greet(_ context.Context, req *greeterv0.HelloRequest) (*greeterv0.HelloReply, error) {
	return &greeterv0.HelloReply{Message: "hello " + req.Name}, nil
}

// Extension publishes the greeter point for socket exposure.
var Extension = extensions.New(extensions.Declaration{
	ID: ID,
	Providers: []extensions.Provider{
		Point.Provide(greeter{}),
		servicev0.Offer(Point),
	},
})
