// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package host_test

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/extensions"
	greeterv0 "github.com/moby/extensions/example/greeter/v0"
	greeterpb "github.com/moby/extensions/example/greeter/v0/protogen"
	servicev0 "github.com/moby/extensions/extpoints/service/v0"
	"github.com/moby/extensions/grpcproxy"
	"github.com/moby/extensions/host"
	echopb "github.com/moby/extensions/internal/launcher/echo/v1/protogen"
	"github.com/moby/extensions/serverpoint"
	"github.com/moby/extensions/servicegrpc"
	"github.com/moby/extensions/testdata/greeter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gotest.tools/v3/assert"
)

// offeredPoints returns the points each extension offers through servicev0.
func offeredPoints(h *host.Host) map[extensions.ExtensionID][]extensions.PointID {
	out := map[extensions.ExtensionID][]extensions.PointID{}
	for _, provider := range h.Providers(servicev0.Point.ID()) {
		out[provider.Identity.ID] = provider.Impl.(servicev0.Provider).OfferedPoints()
	}

	return out
}

// TestPointSocketExposure verifies an out-of-process published Point is
// reachable through a proxy that routes its offered Point's package.
func TestPointSocketExposure(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches a helper binary")
	}

	dir := t.TempDir()
	bin := extensionBinaryPath(dir, greeter.ID)
	build := exec.Command("go", "build", "-o", bin,
		"github.com/moby/extensions/testdata/greeter/cmd/greeter")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build greeter extension: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var policyIdentity extensions.ExtensionIdentity
	var policyPoints []extensions.PointID
	h, err := host.New(ctx,
		host.WithRuntimeDir(shortTempDir(t)),
		host.WithDirs(dir),
		host.WithProviderPolicy(host.PointPolicyFunc(func(identity extensions.ExtensionIdentity, point extensions.PointID) host.PointPolicyResult {
			policyPoints = append(policyPoints, point)
			policyIdentity = identity
			if identity.ID == greeter.ID && point == servicev0.Point.ID() {
				return host.Allow()
			}
			return host.Drop()
		})),
	)
	assert.NilError(t, err)
	defer func() { assert.NilError(t, h.Shutdown(context.WithoutCancel(ctx))) }()
	assert.DeepEqual(t, policyIdentity, extensions.ExtensionIdentity{
		ID: greeter.ID,
		Origin: extensions.ExtensionOrigin{
			Kind:       extensions.ExtensionOriginExecutable,
			Executable: &extensions.ExecutableOrigin{Path: bin},
		},
	})
	assert.DeepEqual(t, policyPoints, []extensions.PointID{servicev0.Point.ID()})

	offered := offeredPoints(h)
	assert.DeepEqual(t, offered, map[extensions.ExtensionID][]extensions.PointID{
		greeter.ID: {greeterv0.Point.ID()},
	})

	var backends []grpcproxy.Backend

	for ext, points := range offered {
		conn, ok := h.Conn(ext)
		assert.Assert(t, ok, "no connection for extension %q", ext)

		backend := grpcproxy.Backend{ID: string(ext), Conn: conn}
		for _, point := range points {
			backend.Packages = append(backend.Packages, string(point))
		}

		backends = append(backends, backend)
	}

	routes, err := grpcproxy.BuildRoutes(backends, nil)
	assert.NilError(t, err)

	sock := filepath.Join(shortTempDir(t), "api.sock")
	lis, err := net.Listen("unix", sock)
	assert.NilError(t, err)
	proxy := grpcproxy.New(routes)
	go func() { _ = proxy.Serve(lis) }()
	defer proxy.Stop()

	conn, err := grpc.NewClient("unix:"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NilError(t, err)
	defer func() { assert.NilError(t, conn.Close()) }()

	resp, err := greeterpb.NewClient(conn).Greet(ctx, &greeterv0.HelloRequest{Name: "world"})
	assert.NilError(t, err)
	assert.Equal(t, resp.Message, "hello world")
}

// TestProcessOfferIsDroppedByDefault verifies a nil Host policy keeps an
// offered Point private without affecting its internal client wiring.
func TestProcessOfferIsDroppedByDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches a helper binary")
	}

	dir := t.TempDir()
	const id = "org.example.exthook.v1"
	bin := extensionBinaryPath(dir, id)
	build := exec.Command("go", "build", "-o", bin, "github.com/moby/extensions/internal/launcher/testdata/exthook")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build exthook extension: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	h, err := host.New(ctx,
		host.WithRuntimeDir(shortTempDir(t)),
		host.WithDirs(dir),
		host.WithClientProviders(echopb.ClientPoint),
	)
	assert.NilError(t, err)
	defer func() { assert.NilError(t, h.Shutdown(context.WithoutCancel(ctx))) }()

	assert.Check(t, offeredPoints(h)[id] == nil)

	conn, ok := h.Conn(id)
	assert.Check(t, ok)
	client := echopb.NewEchoClient(conn)
	resp, err := client.Echo(ctx, &echopb.EchoRequest{Message: "private"})
	assert.NilError(t, err)
	assert.Equal(t, resp.GetMessage(), "private")
}

func TestProcessOfferPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches a helper binary")
	}

	dir := t.TempDir()
	const id = "org.example.exthook.v1"
	bin := extensionBinaryPath(dir, id)
	build := exec.Command("go", "build", "-o", bin, "github.com/moby/extensions/internal/launcher/testdata/exthook")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build exthook extension: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	t.Run("drop", func(t *testing.T) {
		policyCalls := 0
		h, err := host.New(ctx,
			host.WithRuntimeDir(shortTempDir(t)),
			host.WithDirs(dir),
			host.WithProviderPolicy(host.PointPolicyFunc(func(extensions.ExtensionIdentity, extensions.PointID) host.PointPolicyResult {
				policyCalls++
				return host.Drop()
			})),
		)
		assert.NilError(t, err)
		defer func() { assert.NilError(t, h.Shutdown(context.WithoutCancel(ctx))) }()

		assert.Equal(t, policyCalls, 1)
		assert.Check(t, offeredPoints(h)[id] == nil)
	})

	t.Run("reject", func(t *testing.T) {
		cause := errors.New("publication denied")
		h, err := host.New(ctx,
			host.WithRuntimeDir(shortTempDir(t)),
			host.WithDirs(dir),
			host.WithProviderPolicy(host.PointPolicyFunc(func(extensions.ExtensionIdentity, extensions.PointID) host.PointPolicyResult {
				return host.Reject(cause)
			})),
		)
		assert.Assert(t, h == nil)
		assert.Assert(t, errors.Is(err, cause))
		assert.ErrorContains(t, err, `admit provider for extension "org.example.exthook.v1"`)
	})
}

// TestInProcessPointExposure verifies an in-process offered Point can be
// resolved and registered directly on a gRPC server without a process
// boundary.
func TestInProcessPointExposure(t *testing.T) {
	ctx := t.Context()
	var policyIdentity extensions.ExtensionIdentity
	var policyPoints []extensions.PointID
	h, err := host.New(ctx,
		host.WithRuntimeDir(shortTempDir(t)),
		host.WithExtensions(greeter.Extension),
		host.WithProviderPolicy(host.PointPolicyFunc(func(identity extensions.ExtensionIdentity, point extensions.PointID) host.PointPolicyResult {
			policyPoints = append(policyPoints, point)
			policyIdentity = identity
			if identity.ID == greeter.ID && (point == greeterv0.Point.ID() || point == servicev0.Point.ID()) {
				return host.Allow()
			}
			return host.Drop()
		})),
	)
	assert.NilError(t, err)
	defer func() { assert.NilError(t, h.Shutdown(context.WithoutCancel(ctx))) }()
	assert.DeepEqual(t, policyPoints, []extensions.PointID{greeterv0.Point.ID(), servicev0.Point.ID()})
	assert.Equal(t, policyIdentity, extensions.ExtensionIdentity{
		ID:     greeter.ID,
		Origin: extensions.ExtensionOrigin{Kind: extensions.ExtensionOriginBuiltin},
	})
	assert.DeepEqual(t, offeredPoints(h), map[extensions.ExtensionID][]extensions.PointID{
		greeter.ID: {greeterv0.Point.ID()},
	})
	impl, err := h.Provider(greeterv0.Point.ID(), greeter.ID)
	assert.NilError(t, err)

	srv := grpc.NewServer()
	assert.NilError(t, servicegrpc.Register(srv, greeterpb.ServerPoint, impl))
	sock := filepath.Join(shortTempDir(t), "api.sock")
	lis, err := net.Listen("unix", sock)
	assert.NilError(t, err)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("unix:"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NilError(t, err)
	defer func() { assert.NilError(t, conn.Close()) }()

	resp, err := greeterpb.NewClient(conn).Greet(ctx, &greeterv0.HelloRequest{Name: "world"})
	assert.NilError(t, err)
	assert.Equal(t, resp.Message, "hello world")
}

// serveUnix serves on a fresh Unix socket and returns a client connection to
// it.
func serveUnix(t *testing.T, serve func(net.Listener) error, stop func()) *grpc.ClientConn {
	t.Helper()
	sock := filepath.Join(shortTempDir(t), "api.sock")
	lis, err := net.Listen("unix", sock)
	assert.NilError(t, err)

	go func() { _ = serve(lis) }()

	t.Cleanup(stop)

	conn, err := grpc.NewClient("unix:"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NilError(t, err)
	t.Cleanup(func() { assert.NilError(t, conn.Close()) })

	return conn
}

// TestOffersReachableForBuiltinAndProcessExtensions verifies offers of builtin
// and launched extensions in one Host are reachable by clients: launched ones
// through a proxy on their connection, builtin ones served in-process.
func TestOffersReachableForBuiltinAndProcessExtensions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches a helper binary")
	}

	dir := t.TempDir()

	const processID = "org.example.exthook.v1"

	bin := extensionBinaryPath(dir, processID)

	build := exec.Command("go", "build", "-o", bin, "github.com/moby/extensions/internal/launcher/testdata/exthook")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build exthook extension: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	h, err := host.New(ctx,
		host.WithRuntimeDir(shortTempDir(t)),
		host.WithDirs(dir),
		host.WithExtensions(greeter.Extension),
		host.WithProviderPolicy(host.PointPolicyFunc(func(extensions.ExtensionIdentity, extensions.PointID) host.PointPolicyResult {
			return host.Allow()
		})),
	)

	assert.NilError(t, err)
	defer func() { assert.NilError(t, h.Shutdown(context.WithoutCancel(ctx))) }()

	servers := map[extensions.PointID]serverpoint.Registration{greeterv0.Point.ID(): greeterpb.ServerPoint}
	inProcess := grpc.NewServer()

	var backends []grpcproxy.Backend

	for id, points := range offeredPoints(h) {
		if conn, ok := h.Conn(id); ok {
			backend := grpcproxy.Backend{ID: string(id), Conn: conn}
			for _, point := range points {
				backend.Packages = append(backend.Packages, string(point))
			}

			backends = append(backends, backend)

			continue
		}

		for _, point := range points {
			impl, err := h.Provider(point, id)
			assert.NilError(t, err)
			assert.NilError(t, servicegrpc.Register(inProcess, servers[point], impl))
		}
	}

	routes, err := grpcproxy.BuildRoutes(backends, nil)
	assert.NilError(t, err)

	proxy := grpcproxy.New(routes)

	greet, err := greeterpb.NewClient(serveUnix(t, inProcess.Serve, inProcess.Stop)).Greet(ctx, &greeterv0.HelloRequest{Name: "world"})
	assert.NilError(t, err)
	assert.Equal(t, greet.Message, "hello world")

	echo, err := echopb.NewEchoClient(serveUnix(t, proxy.Serve, proxy.Stop)).Echo(ctx, &echopb.EchoRequest{Message: "offered"})
	assert.NilError(t, err)
	assert.Equal(t, echo.GetMessage(), "offered")
}
