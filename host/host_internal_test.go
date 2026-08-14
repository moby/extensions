package host

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/moby/extensions"
	"github.com/moby/extensions/clientpoint"
	"github.com/moby/extensions/internal/broker"
	"github.com/moby/extensions/internal/launcher"
	echopb "github.com/moby/extensions/internal/launcher/echo/v1/protogen"
	"github.com/moby/extensions/serverpoint"
	"google.golang.org/grpc"
	"gotest.tools/v3/assert"
)

const lifecycleExtensionID = extensions.ExtensionID("org.example.lifecycle.v1")

func shortTempDir(t *testing.T) string {
	t.Helper()
	// Keep socket paths relative so they fit Windows' AF_UNIX path limit.
	dir, err := os.MkdirTemp(".", "m")
	assert.NilError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func buildLifecycleExtension(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	name := string(lifecycleExtensionID)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin = filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", bin,
		"github.com/moby/extensions/host/testdata/lifecycle")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build lifecycle extension: %v\n%s", err, out)
	}
	return dir, bin
}

func processProbeConfig(probeFile string, failInit bool) map[extensions.ExtensionID]extensions.Config {
	return map[extensions.ExtensionID]extensions.Config{
		lifecycleExtensionID: {
			"probeFile": probeFile,
			"failInit":  failInit,
		},
	}
}

func processProbeAddress(t *testing.T, probeFile string) string {
	t.Helper()
	address, err := os.ReadFile(probeFile)
	assert.NilError(t, err)
	return string(address)
}

func assertProcessRunning(t *testing.T, probeFile string) {
	t.Helper()
	listener, err := net.Listen("tcp", processProbeAddress(t, probeFile))
	if err == nil {
		_ = listener.Close()
		t.Fatal("process probe was available while the extension should be running")
	}
}

func assertProcessReleased(t *testing.T, probeFile string) {
	t.Helper()
	listener, err := net.Listen("tcp", processProbeAddress(t, probeFile))
	assert.NilError(t, err, "process probe was not released")
	assert.NilError(t, listener.Close())
}

func TestExtensionFromLaunchedRejectsUnsupportedPoints(t *testing.T) {
	const supported = extensions.PointID("org.mobyproject.extension.supported.v1")
	const unsupported = extensions.PointID("org.example.own.api.v1")

	providers := map[extensions.PointID]clientpoint.Provider{
		supported: func(grpc.ClientConnInterface) extensions.Provider {
			return extensions.Provider{Point: supported, Impl: "impl"}
		},
	}

	ext, err := extensionFromLaunched(&launcher.Launched{
		ID:     "org.example.ext.v1",
		Points: []launcher.LaunchedPoint{{ID: supported}},
	}, providers, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(ext.Declaration().Providers), 1)

	_, err = extensionFromLaunched(&launcher.Launched{
		ID:     "org.example.ext.v1",
		Points: []launcher.LaunchedPoint{{ID: supported}, {ID: unsupported}},
	}, providers, nil)
	assert.ErrorContains(t, err, "unsupported point")
	assert.ErrorContains(t, err, string(unsupported))

	ext, err = extensionFromLaunched(&launcher.Launched{
		ID:     "org.example.ext.v1",
		Points: []launcher.LaunchedPoint{{ID: supported}, {ID: unsupported}},
	}, providers, map[extensions.PointID]bool{unsupported: true})
	assert.NilError(t, err)
	assert.Equal(t, len(ext.Declaration().Providers), 1)
}

func TestClientProviderMap(t *testing.T) {
	const pointA = extensions.PointID("org.example.a.v1")
	const pointB = extensions.PointID("org.example.b.v1")
	build := func(grpc.ClientConnInterface) extensions.Provider {
		return extensions.Provider{}
	}

	m, err := clientProviderMap([]clientpoint.Registration{
		{Point: pointA, Provider: build},
		{Point: pointB, Provider: build},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(m), 2)
	_, okA := m[pointA]
	_, okB := m[pointB]
	assert.Assert(t, okA)
	assert.Assert(t, okB)

	_, err = clientProviderMap([]clientpoint.Registration{
		{Point: pointA, Provider: build},
		{Point: pointA, Provider: build},
	})
	assert.ErrorContains(t, err, "duplicate client provider")
	assert.ErrorContains(t, err, string(pointA))
}

func newProviderExtension(id extensions.ExtensionID, point extensions.PointID) extensions.Extension {
	return extensions.New(extensions.Declaration{
		ID:        id,
		Providers: []extensions.Provider{{Point: point, Impl: "impl"}},
	})
}

func TestServeCallback(t *testing.T) {
	const dep = extensions.PointID("org.mobyproject.extension.dep.v1")

	newDep := func(served *[]any) serverpoint.Registration {
		return serverpoint.Registration{
			Point: dep,
			Register: func(_ grpc.ServiceRegistrar, impl any) {
				*served = append(*served, impl)
			},
		}
	}

	t.Run("zero providers is skipped", func(t *testing.T) {
		b := broker.New()
		var served []any
		endpoint := filepath.Join(shortTempDir(t), "callback.sock")
		srv, err := serveCallback(endpoint, []serverpoint.Registration{newDep(&served)}, b)
		assert.NilError(t, err)
		if srv != nil {
			defer srv.Stop()
		}
		assert.Equal(t, len(served), 0)
	})

	t.Run("one provider is registered", func(t *testing.T) {
		b := broker.New()
		assert.NilError(t, b.Register(newProviderExtension("org.example.a.v1", dep)))
		var served []any
		endpoint := filepath.Join(shortTempDir(t), "callback.sock")
		srv, err := serveCallback(endpoint, []serverpoint.Registration{newDep(&served)}, b)
		assert.NilError(t, err)
		assert.Assert(t, srv != nil)
		defer srv.Stop()
		assert.Equal(t, len(served), 1)
	})

	t.Run("multiple providers is an error", func(t *testing.T) {
		b := broker.New()
		assert.NilError(t, b.Register(newProviderExtension("org.example.a.v1", dep)))
		assert.NilError(t, b.Register(newProviderExtension("org.example.b.v1", dep)))
		var served []any
		endpoint := filepath.Join(shortTempDir(t), "callback.sock")
		srv, err := serveCallback(endpoint, []serverpoint.Registration{newDep(&served)}, b)
		if srv != nil {
			srv.Stop()
		}
		assert.ErrorContains(t, err, string(dep))
		assert.Equal(t, len(served), 0)
	})
}

func TestSinglePointRejectsTwoProviders(t *testing.T) {
	const point = extensions.PointID("org.example.decider.v1")
	ext := func(id extensions.ExtensionID) extensions.Extension {
		return extensions.New(extensions.Declaration{
			ID:        id,
			Providers: []extensions.Provider{{Point: point, Impl: struct{}{}}},
		})
	}
	singleReg := clientpoint.Registration{
		Point:    point,
		Provider: func(grpc.ClientConnInterface) extensions.Provider { return extensions.Provider{} },
		Single:   true,
	}

	_, err := New(context.Background(), Options{
		RuntimeDir:      t.TempDir(),
		Extensions:      []extensions.Extension{ext("org.example.one.v1"), ext("org.example.two.v1")},
		ClientProviders: []clientpoint.Registration{singleReg},
	})
	assert.ErrorContains(t, err, `point "org.example.decider.v1" admits a single provider`)
	assert.ErrorContains(t, err, "org.example.one.v1")
	assert.ErrorContains(t, err, "org.example.two.v1")

	h, err := New(context.Background(), Options{
		RuntimeDir:      t.TempDir(),
		Extensions:      []extensions.Extension{ext("org.example.one.v1")},
		ClientProviders: []clientpoint.Registration{singleReg},
	})
	assert.NilError(t, err)
	assert.NilError(t, h.Shutdown(context.Background()))
}

// TestLaunchedExtensionCarriesShutdown verifies launched extensions participate
// in broker shutdown ordering.
func TestLaunchedExtensionCarriesShutdown(t *testing.T) {
	const point = extensions.PointID("org.mobyproject.extension.supported.v1")
	providers := map[extensions.PointID]clientpoint.Provider{
		point: func(grpc.ClientConnInterface) extensions.Provider {
			return extensions.Provider{Point: point, Impl: "impl"}
		},
	}

	ext, err := extensionFromLaunched(&launcher.Launched{
		ID:     "org.example.ext.v1",
		Points: []launcher.LaunchedPoint{{ID: point}},
	}, providers, nil)
	assert.NilError(t, err)
	assert.Assert(t, ext.Declaration().Shutdown != nil,
		"a launched extension must declare a Shutdown so the broker stops it in dependency order")
}

func TestProcessResourceCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches a helper binary")
	}
	dir, bin := buildLifecycleExtension(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("adaptation error", func(t *testing.T) {
		probeFile := filepath.Join(t.TempDir(), "probe")
		_, _, err := loadProcess(ctx, launcher.Launcher{
			RuntimeDir:      shortTempDir(t),
			ExtensionConfig: processProbeConfig(probeFile, false),
		}, bin, nil, nil)
		assert.ErrorContains(t, err, "unsupported point")
		assertProcessReleased(t, probeFile)
	})

	t.Run("register error", func(t *testing.T) {
		probeFile := filepath.Join(t.TempDir(), "probe")
		_, err := New(ctx, Options{
			RuntimeDir: shortTempDir(t),
			Extensions: []extensions.Extension{extensions.New(extensions.Declaration{
				ID: lifecycleExtensionID,
			})},
			Dirs:            []string{dir},
			ClientProviders: []clientpoint.Registration{echopb.ClientPoint},
			ExtensionConfig: processProbeConfig(probeFile, false),
		})
		assert.ErrorContains(t, err, "already registered")
		assertProcessReleased(t, probeFile)
	})

	t.Run("partial init error", func(t *testing.T) {
		probeFile := filepath.Join(t.TempDir(), "probe")
		semanticCloseErr := errors.New("semantic cleanup failure")
		semanticShutdownCalled := false
		processRunningDuringSemanticShutdown := false
		var probeObservationErr error
		initializedBuiltin := extensions.New(extensions.Declaration{
			ID: "org.example.initialized.v1",
			Init: func(context.Context, extensions.Config, extensions.Resolver) error {
				return nil
			},
			Shutdown: func(context.Context) error {
				semanticShutdownCalled = true
				// Construction cleanup must run broker shutdown before releasing the
				// process resource.
				address, err := os.ReadFile(probeFile)
				if err != nil {
					probeObservationErr = err
					return semanticCloseErr
				}
				listener, err := net.Listen("tcp", string(address))
				processRunningDuringSemanticShutdown = err != nil
				if listener != nil {
					_ = listener.Close()
				}
				return semanticCloseErr
			},
		})

		_, err := New(ctx, Options{
			RuntimeDir:      shortTempDir(t),
			Extensions:      []extensions.Extension{initializedBuiltin},
			Dirs:            []string{dir},
			ClientProviders: []clientpoint.Registration{echopb.ClientPoint},
			ExtensionConfig: processProbeConfig(probeFile, true),
		})
		assert.ErrorContains(t, err, "requested initialization failure")
		assert.Assert(t, semanticShutdownCalled)
		assert.NilError(t, probeObservationErr)
		assert.Assert(t, processRunningDuringSemanticShutdown,
			"process resource was released before semantic broker shutdown")
		assert.Assert(t, !errors.Is(err, semanticCloseErr),
			"construction cleanup error replaced or was joined with the init error")
		assertProcessReleased(t, probeFile)
	})

	t.Run("normal shutdown", func(t *testing.T) {
		probeFile := filepath.Join(t.TempDir(), "probe")
		h, err := New(ctx, Options{
			RuntimeDir: shortTempDir(t),
			Extensions: []extensions.Extension{extensions.New(extensions.Declaration{
				ID: "org.example.builtin.v1",
			})},
			Dirs:            []string{dir},
			ClientProviders: []clientpoint.Registration{echopb.ClientPoint},
			ExtensionConfig: processProbeConfig(probeFile, false),
		})
		assert.NilError(t, err)
		shutdown := false
		t.Cleanup(func() {
			if !shutdown {
				_ = h.Shutdown(context.Background())
			}
		})
		assert.Equal(t, len(h.loaded), 1,
			"only the process-backed extension should own a loaded resource")
		assertProcessRunning(t, probeFile)

		err = h.Shutdown(context.Background())
		shutdown = true
		assert.NilError(t, err)
		assertProcessReleased(t, probeFile)
	})
}

func TestCloseLoadedErrClosesInReverseOrderAndJoinsErrors(t *testing.T) {
	firstErr := errors.New("first close")
	secondErr := errors.New("second close")
	var closed []string
	loaded := []loadedExtension{
		{close: func(context.Context) error {
			closed = append(closed, "first")
			return firstErr
		}},
		{close: func(context.Context) error {
			closed = append(closed, "second")
			return secondErr
		}},
	}

	err := closeLoadedErr(context.Background(), loaded)
	assert.DeepEqual(t, closed, []string{"second", "first"})
	assert.Assert(t, errors.Is(err, firstErr))
	assert.Assert(t, errors.Is(err, secondErr))
}

func TestCloseLoadedSuppressesConstructionCleanupErrors(t *testing.T) {
	closeErr := errors.New("close failure")
	var closed []string
	closeLoaded(context.Background(), []loadedExtension{
		{close: func(context.Context) error {
			closed = append(closed, "first")
			return closeErr
		}},
		{close: func(context.Context) error {
			closed = append(closed, "second")
			return closeErr
		}},
	})
	assert.DeepEqual(t, closed, []string{"second", "first"})
}

func TestHostShutdownJoinsSemanticAndResourceErrors(t *testing.T) {
	semanticErr := errors.New("semantic shutdown")
	resourceErr := errors.New("resource close")
	var order []string
	b := broker.New()
	assert.NilError(t, b.Register(extensions.New(extensions.Declaration{
		ID: "org.example.shutdown.v1",
		Shutdown: func(context.Context) error {
			order = append(order, "semantic")
			return semanticErr
		},
	})))
	assert.NilError(t, b.Init(context.Background(), nil))
	h := &Host{
		broker: b,
		loaded: []loadedExtension{{close: func(context.Context) error {
			order = append(order, "resource")
			return resourceErr
		}}},
	}

	err := h.Shutdown(context.Background())
	assert.DeepEqual(t, order, []string{"semantic", "resource"})
	assert.Assert(t, errors.Is(err, semanticErr))
	assert.Assert(t, errors.Is(err, resourceErr))
}

func TestServicesForPointUsesProcessPublicationIndex(t *testing.T) {
	const point = extensions.PointID("org.example.publication.v1")
	h := &Host{processServices: map[extensions.ExtensionID]map[extensions.PointID][]string{
		"org.example.first.v1": {
			point: {"example.First", "example.Second"},
		},
		"org.example.empty.v1": {
			point: nil,
		},
	}}

	assert.DeepEqual(t, h.ServicesForPoint(point), map[extensions.ExtensionID][]string{
		"org.example.first.v1": {"example.First", "example.Second"},
	})
	assert.DeepEqual(t, h.ServicesForPoint("org.example.unknown.v1"),
		map[extensions.ExtensionID][]string{})
}
