package main

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func parseSource(t *testing.T, src string) (point, error) {
	t.Helper()
	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "contract.go"), []byte(src), 0o644))
	return parsePoint(dir)
}

func TestContractValidation(t *testing.T) {
	const header = `package p
import "github.com/moby/extensions"
type S interface{ Do(ctx interface{}, req *Req) (*Resp, error) }
type Resp struct{ Ok bool ` + "`pb:\"1\"`" + ` }
//mobyextgen:service=Service
var Point = extensions.DefinePoint[S]("test.gen.v1")
`
	cases := []struct {
		name    string
		req     string
		wantErr string
	}{
		{
			name:    "float map key",
			req:     "type Req struct{ M map[float64]string `pb:\"1\"` }",
			wantErr: "map keys must be strings",
		},
		{
			name:    "int map key",
			req:     "type Req struct{ M map[int32]string `pb:\"1\"` }",
			wantErr: "map keys must be strings",
		},
		{
			name:    "non-numeric field number",
			req:     "type Req struct{ Name string `pb:\"one\"` }",
			wantErr: "not a field number",
		},
		{
			name:    "empty field number",
			req:     "type Req struct{ Name string `pb:\"\"` }",
			wantErr: "not a field number",
		},
		{
			name:    "zero field number",
			req:     "type Req struct{ Name string `pb:\"0\"` }",
			wantErr: "must be >= 1",
		},
		{
			name:    "oversized field number",
			req:     "type Req struct{ Name string `pb:\"536870912\"` }",
			wantErr: "must be <= 536870911",
		},
		{
			name:    "protobuf implementation-reserved field number",
			req:     "type Req struct{ Name string `pb:\"19000\"` }",
			wantErr: "reserved by the protobuf implementation",
		},
		{
			name:    "maximum field number",
			req:     "type Req struct{ Name string `pb:\"536870911\"` }",
			wantErr: "",
		},
		{
			name:    "duplicate field number",
			req:     "type Req struct{ A string `pb:\"1\"`; B string `pb:\"1\"` }",
			wantErr: "used by both",
		},
		{
			name:    "valid string map",
			req:     "type Req struct{ M map[string]string `pb:\"1\"` }",
			wantErr: "",
		},
		{
			name:    "width-ambiguous int field",
			req:     "type Req struct{ Count int `pb:\"1\"` }",
			wantErr: "no fixed width on the wire",
		},
		{
			name:    "width-ambiguous uint slice",
			req:     "type Req struct{ Counts []uint `pb:\"1\"` }",
			wantErr: "no fixed width on the wire",
		},
		{
			name:    "width-ambiguous int map value",
			req:     "type Req struct{ M map[string]int `pb:\"1\"` }",
			wantErr: "no fixed width on the wire",
		},
		{
			name:    "sized int field",
			req:     "type Req struct{ Count int64 `pb:\"1\"` }",
			wantErr: "",
		},
		{
			name:    "grouped fields share a pb tag",
			req:     "type Req struct{ A, B string `pb:\"1\"` }",
			wantErr: "share a single pb tag",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSource(t, header+tc.req+"\n")
			if tc.wantErr == "" {
				assert.NilError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestServicePragma(t *testing.T) {
	const contract = `package p
import "github.com/moby/extensions"
type S interface{ Do(ctx interface{}, req *Req) (*Resp, error) }
type Req struct{ Name string ` + "`pb:\"1\"`" + ` }
type Resp struct{ Ok bool ` + "`pb:\"1\"`" + ` }
`
	t.Run("names the service", func(t *testing.T) {
		pt, err := parseSource(t, contract+"//mobyextgen:service=SomeHook\nvar Point = extensions.DefinePoint[S](\"test.gen.v1\")\n")
		assert.NilError(t, err)
		assert.Equal(t, pt.service, "SomeHook")
		assert.Equal(t, pt.grpcService(), "test.gen.v1.SomeHook")
	})

	t.Run("is required", func(t *testing.T) {
		_, err := parseSource(t, contract+"var Point = extensions.DefinePoint[S](\"test.gen.v1\")\n")
		assert.ErrorContains(t, err, "no service pragma found")
	})

	t.Run("rejects conflicting names", func(t *testing.T) {
		_, err := parseSource(t, contract+"//mobyextgen:service=A\n//mobyextgen:service=B\nvar Point = extensions.DefinePoint[S](\"test.gen.v1\")\n")
		assert.ErrorContains(t, err, "conflicting")
	})
}

func TestServicePragmaOnANonInterface(t *testing.T) {
	_, err := parseSource(t, `package p
type Req struct{ Name string `+"`pb:\"1\"`"+` }
type Resp struct{ Ok bool `+"`pb:\"1\"`"+` }
type S interface{ Do(ctx interface{}, req *Req) (*Resp, error) }

//mobyextgen:service=my.pkg.v1.Thing
type Thing struct{}
`)
	assert.ErrorContains(t, err, "may only document an interface or the point")
}
