// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

func streamingPointSource(body string) string {
	return `package contract

import (
	stdctx "context"
	ext "github.com/moby/extensions"
	s "github.com/moby/extensions/stream"
)
` + body + `
var Point = ext.DefinePoint[Service]("org.example.contract.v1")
`
}

func TestParseContractStreamingContracts(t *testing.T) {
	t.Parallel()
	source := streamingPointSource(`
// Service documents its streaming operations.
type Service interface {
	// Download sends chunks to the caller.
	Download(stdctx.Context, *DownloadRequest, s.Sender[DownloadChunk]) error
}

type DownloadRequest struct{}
type DownloadChunk struct{}
`)
	point, err := parseSource(t, source)
	assert.NilError(t, err)
	assert.Equal(t, point.doc, "Service documents its streaming operations.")
	assert.Equal(t, len(point.methods), 1)
	assert.Equal(t, point.methods[0].kind, methodServerStreaming)
	assert.Equal(t, point.methods[0].request, "DownloadRequest")
	assert.Equal(t, point.methods[0].response, "DownloadChunk")
	assert.Equal(t, point.methods[0].bareError, false)
	assert.Equal(t, point.methods[0].doc, "Download sends chunks to the caller.")
	messageNames := make(map[string]bool, len(point.messages))
	for _, message := range point.messages {
		messageNames[message.name] = true
	}
	for _, name := range []string{"DownloadRequest", "DownloadChunk"} {
		assert.Assert(t, messageNames[name], "missing message %s", name)
	}
}

func TestParseContractRejectsInvalidStreamingContracts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "server stream with a reply",
			source: streamingPointSource(`type Service interface {
	Download(stdctx.Context, *Request, s.Sender[Chunk]) (*Request, error)
}
type Request struct{}
type Chunk struct{}
`),
			want: acceptedMethodShapes,
		},
		{
			name: "stream in a result",
			source: streamingPointSource(`type Service interface {
	Download(stdctx.Context, *Request) (s.Sender[Chunk], error)
}
type Request struct{}
type Chunk struct{}
`),
			want: acceptedMethodShapes,
		},
		{
			name: "pointer stream message argument",
			source: streamingPointSource(`type Service interface {
	Download(stdctx.Context, *Request, s.Sender[*Chunk]) error
}
type Request struct{}
type Chunk struct{}
`),
			want: "must not be a pointer",
		},
		{
			name: "imported stream message argument",
			source: `package contract

import (
	stdctx "context"
	other "example/other"
	ext "github.com/moby/extensions"
	s "github.com/moby/extensions/stream"
)
type Service interface {
	Download(stdctx.Context, *Request, s.Sender[other.Chunk]) error
}
type Request struct{}
var Point = ext.DefinePoint[Service]("org.example.contract.v1")
`,
			want: "imported message types",
		},
		{
			name: "wrong stream import",
			source: `package contract

import (
	stdctx "context"
	ext "github.com/moby/extensions"
	s "example/stream"
)
type Service interface {
	Download(stdctx.Context, *Request, s.Sender[Chunk]) error
}
type Request struct{}
type Chunk struct{}
var Point = ext.DefinePoint[Service]("org.example.contract.v1")
`,
			want: "must be imported from",
		},
		{
			name: "generic stream message",
			source: streamingPointSource(`type Service interface {
	Download(stdctx.Context, *Request, s.Sender[Chunk]) error
}
type Request struct{}
type Chunk[T any] struct{}
`),
			want: "must not be generic",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseSource(t, test.source)
			assert.Assert(t, is.ErrorContains(err, test.want))
		})
	}
}

func TestStreamingProtoDescriptorAndWire(t *testing.T) {
	t.Parallel()
	source := streamingPointSource(`
// Service documents its streaming operations.
type Service interface {
	// Download sends chunks to the caller.
	Download(stdctx.Context, *DownloadRequest, s.Sender[DownloadChunk]) error
}

type DownloadRequest struct{}
type DownloadChunk struct{}
`)
	point, err := parseSource(t, source)
	assert.NilError(t, err)
	point.importPath = "example.test/contract"
	point.protoPath = "contract/service.proto"

	protoFile, err := emitProto(point)
	assert.NilError(t, err)
	protoText := string(protoFile)
	for _, declaration := range []string{
		"rpc Download(DownloadRequest) returns (stream DownloadChunk);",
	} {
		assert.Assert(t, strings.Contains(protoText, declaration), declaration)
	}

	fd, err := fileDescriptor(point)
	assert.NilError(t, err)
	assert.Equal(t, len(fd.GetService()), 1)
	methods := fd.GetService()[0].GetMethod()
	assert.Equal(t, len(methods), 1)
	assert.Equal(t, methods[0].GetName(), "Download")
	assert.Equal(t, methods[0].GetServerStreaming(), true)
	assert.Equal(t, methods[0].GetClientStreaming(), false)
	assert.Assert(t, methods[0].ClientStreaming == nil)

	wire, err := emitWire(point)
	assert.NilError(t, err)
	wireText := string(wire)
	for _, declaration := range []string{
		"Download(*DownloadRequest, grpc.ServerStreamingServer[DownloadChunk]) error",
		"Streams: []grpc.StreamDesc{",
		"ServerStreams: true",
	} {
		assert.Assert(t, strings.Contains(wireText, declaration), declaration)
	}
}
