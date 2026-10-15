// SPDX-FileCopyrightText: Copyright The Moby Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/parser"
	"go/token"
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
	// Upload receives chunks from the caller.
	Upload(stdctx.Context, s.Receiver[UploadChunk]) (*UploadReply, error)
}

type DownloadRequest struct{}
type DownloadChunk struct{}
type UploadChunk struct{}
type UploadReply struct{}
`)
	point, err := parseSource(t, source)
	assert.NilError(t, err)
	assert.Equal(t, point.doc, "Service documents its streaming operations.")
	assert.Equal(t, len(point.methods), 2)
	assert.Equal(t, point.methods[0].kind, methodServerStreaming)
	assert.Equal(t, point.methods[0].request, "DownloadRequest")
	assert.Equal(t, point.methods[0].response, "DownloadChunk")
	assert.Equal(t, point.methods[0].bareError, false)
	assert.Equal(t, point.methods[0].doc, "Download sends chunks to the caller.")
	assert.Equal(t, point.methods[1].kind, methodClientStreaming)
	assert.Equal(t, point.methods[1].request, "UploadChunk")
	assert.Equal(t, point.methods[1].response, "UploadReply")
	assert.Equal(t, point.methods[1].doc, "Upload receives chunks from the caller.")
	messageNames := make(map[string]bool, len(point.messages))
	for _, message := range point.messages {
		messageNames[message.name] = true
	}
	for _, name := range []string{"DownloadRequest", "DownloadChunk", "UploadChunk", "UploadReply"} {
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
			name: "unsupported parameter shape",
			source: streamingPointSource(`type Service interface {
	Sync(stdctx.Context, s.Receiver[Chunk], s.Sender[Chunk]) error
}
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
			name: "client stream without a context",
			source: streamingPointSource(`type Service interface {
	Upload(*Request, s.Receiver[Chunk]) (*Reply, error)
}
type Request struct{}
type Chunk struct{}
type Reply struct{}
`),
			want: "parameter 1: must be context.Context",
		},
		{
			name: "server stream with a non-context first parameter",
			source: streamingPointSource(`type Service interface {
	Download(int, *Request, s.Sender[Chunk]) error
}
type Request struct{}
type Chunk struct{}
`),
			want: "parameter 1: must be context.Context",
		},
		{
			name: "context from another package",
			source: `package contract

import (
	stdctx "example/context"
	ext "github.com/moby/extensions"
	s "github.com/moby/extensions/stream"
)
type Service interface {
	Download(stdctx.Context, *Request, s.Sender[Chunk]) error
}
type Request struct{}
type Chunk struct{}
var Point = ext.DefinePoint[Service]("org.example.contract.v1")
`,
			want: "parameter 1: must be context.Context",
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

func TestStreamingHelpersDoNotCollideWithHandlers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		methods string
	}{
		{
			name: "server stream beside a unary Sender method",
			methods: `Handle(stdctx.Context, *Request, s.Sender[Chunk]) error
	Sender(stdctx.Context, *Request) error`,
		},
		{
			name: "client stream beside a unary Receiver method",
			methods: `Handle(stdctx.Context, s.Receiver[Chunk]) (*Request, error)
	Receiver(stdctx.Context, *Request) error`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			point, err := parseSource(t, streamingPointSource("type Service interface {\n\t"+test.methods+`
}
type Request struct{}
type Chunk struct{}
`))
			assert.NilError(t, err)
			point.importPath = "example.test/contract"
			wire, err := emitWire(point)
			assert.NilError(t, err)
			_, err = parser.ParseFile(token.NewFileSet(), "wire.gen.go", wire, parser.DeclarationErrors)
			assert.NilError(t, err)
		})
	}
}

func TestStreamingProtoDescriptorAndWire(t *testing.T) {
	t.Parallel()
	source := streamingPointSource(`
// Service documents both streaming directions.
type Service interface {
	// Download sends chunks to the caller.
	Download(stdctx.Context, *DownloadRequest, s.Sender[DownloadChunk]) error
	// Upload receives chunks from the caller.
	Upload(stdctx.Context, s.Receiver[UploadChunk]) (*UploadReply, error)
}

type DownloadRequest struct{}
type DownloadChunk struct{}
type UploadChunk struct{}
type UploadReply struct{}
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
		"rpc Upload(stream UploadChunk) returns (UploadReply);",
	} {
		assert.Assert(t, strings.Contains(protoText, declaration), declaration)
	}

	fd, err := fileDescriptor(point)
	assert.NilError(t, err)
	assert.Equal(t, len(fd.GetService()), 1)
	methods := fd.GetService()[0].GetMethod()
	assert.Equal(t, len(methods), 2)
	assert.Equal(t, methods[0].GetName(), "Download")
	assert.Equal(t, methods[0].GetServerStreaming(), true)
	assert.Equal(t, methods[0].GetClientStreaming(), false)
	assert.Assert(t, methods[0].ClientStreaming == nil)
	assert.Equal(t, methods[1].GetName(), "Upload")
	assert.Equal(t, methods[1].GetServerStreaming(), false)
	assert.Equal(t, methods[1].GetClientStreaming(), true)
	assert.Assert(t, methods[1].ServerStreaming == nil)

	wire, err := emitWire(point)
	assert.NilError(t, err)
	wireText := string(wire)
	for _, declaration := range []string{
		"Download(*DownloadRequest, grpc.ServerStreamingServer[DownloadChunk]) error",
		"Upload(grpc.ClientStreamingServer[UploadChunk, UploadReply]) error",
		"Streams: []grpc.StreamDesc{",
		"ServerStreams: true",
		"ClientStreams: true",
	} {
		assert.Assert(t, strings.Contains(wireText, declaration), declaration)
	}
}
