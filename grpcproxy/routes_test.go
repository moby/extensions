// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package grpcproxy

import (
	"testing"

	"google.golang.org/grpc"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

type connStub struct{ grpc.ClientConnInterface }

func TestBuildRoutesDisjoint(t *testing.T) {
	a, b := &connStub{}, &connStub{}
	routes, err := BuildRoutes([]Backend{
		{ID: "ext.a", Conn: a, Services: []string{"a.Svc1", "a.Svc2"}},
		{ID: "ext.b", Conn: b, Services: []string{"b.Svc"}},
	}, nil)
	assert.NilError(t, err)
	assert.Equal(t, routes.Len(), 3)
	assert.Equal(t, routes.Services["a.Svc1"], grpc.ClientConnInterface(a))
	assert.Equal(t, routes.Services["a.Svc2"], grpc.ClientConnInterface(a))
	assert.Equal(t, routes.Services["b.Svc"], grpc.ClientConnInterface(b))
}

func TestBuildRoutesRejectsExtensionConflict(t *testing.T) {
	_, err := BuildRoutes([]Backend{
		{ID: "ext.z", Conn: &connStub{}, Services: []string{"shared.Svc"}},
		{ID: "ext.a", Conn: &connStub{}, Services: []string{"shared.Svc"}},
	}, nil)
	assert.ErrorContains(t, err, `"ext.a" and "ext.z" both expose gRPC service "shared.Svc"`)
}

func TestBuildRoutesRejectsReservedConflict(t *testing.T) {
	reserved := map[string]struct{}{"moby.buildkit.v1.Control": {}}
	_, err := BuildRoutes([]Backend{
		{ID: "ext.evil", Conn: &connStub{}, Services: []string{"moby.buildkit.v1.Control"}},
	}, reserved)
	assert.ErrorContains(t, err, `backend "ext.evil" cannot expose gRPC service "moby.buildkit.v1.Control"`)
	assert.Check(t, is.Contains(err.Error(), "reserved"))
}

func TestBuildRoutesEmpty(t *testing.T) {
	routes, err := BuildRoutes(nil, nil)
	assert.NilError(t, err)
	assert.Equal(t, routes.Len(), 0)
}

func TestBuildRoutesPackages(t *testing.T) {
	a := &connStub{}
	routes, err := BuildRoutes([]Backend{
		{ID: "ext.a", Conn: a, Services: []string{"a.v1.Extra"}, Packages: []string{"a.v1"}},
	}, nil)
	assert.NilError(t, err, "a backend may list services inside its own package")
	assert.Equal(t, routes.Packages["a.v1"], grpc.ClientConnInterface(a))

	for _, tc := range []struct {
		name     string
		backends []Backend
		reserved map[string]struct{}
		want     string
	}{
		{
			name: "runtime service",
			backends: []Backend{
				{ID: "ext.a", Services: []string{"moby.extension.runtime.v1.Extension"}},
			},
			want: `backend "ext.a" cannot expose gRPC service "moby.extension.runtime.v1.Extension"`,
		},
		{
			name: "runtime package",
			backends: []Backend{
				{ID: "ext.a", Packages: []string{"moby.extension.runtime.v1"}},
			},
			want: `backend "ext.a" cannot expose package "moby.extension.runtime.v1"`,
		},
		{
			name: "same package",
			backends: []Backend{
				{ID: "ext.z", Conn: &connStub{}, Packages: []string{"shared.v1"}},
				{ID: "ext.a", Conn: &connStub{}, Packages: []string{"shared.v1"}},
			},
			want: `backends "ext.a" and "ext.z" both expose package "shared.v1"`,
		},
		{
			name: "package over another backend's service",
			backends: []Backend{
				{ID: "ext.a", Conn: &connStub{}, Services: []string{"shared.v1.Svc"}},
				{ID: "ext.z", Conn: &connStub{}, Packages: []string{"shared.v1"}},
			},
			want: `backend "ext.z" cannot expose package "shared.v1": backend "ext.a" exposes its service "shared.v1.Svc"`,
		},
		{
			name: "service inside another backend's package",
			backends: []Backend{
				{ID: "ext.a", Conn: &connStub{}, Packages: []string{"shared.v1"}},
				{ID: "ext.z", Conn: &connStub{}, Services: []string{"shared.v1.Svc"}},
			},
			want: `backend "ext.z" cannot expose gRPC service "shared.v1.Svc": backend "ext.a" exposes its package`,
		},
		{
			name: "package containing a reserved service",
			backends: []Backend{
				{ID: "ext.evil", Conn: &connStub{}, Packages: []string{"moby.buildkit.v1"}},
			},
			reserved: map[string]struct{}{"moby.buildkit.v1.Control": {}},
			want:     `backend "ext.evil" cannot expose package "moby.buildkit.v1": it contains already-served service "moby.buildkit.v1.Control"`,
		},
		{
			name: "empty package",
			backends: []Backend{
				{ID: "ext.a", Conn: &connStub{}, Packages: []string{""}},
			},
			want: `backend "ext.a" cannot expose an empty package`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildRoutes(tc.backends, tc.reserved)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}
