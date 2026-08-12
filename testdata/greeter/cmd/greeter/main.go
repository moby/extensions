// Command greeter serves the greeter fixture as an out-of-process extension
// that opts into socket exposure, for the integration test.
package main

import (
	servicegrpcv0 "github.com/moby/extensions/extpoints/servicegrpc/v0"
	"github.com/moby/extensions/sdk"
	"github.com/moby/extensions/testdata/greeter"
)

func main() {
	sdk.Main(greeter.Extension, servicegrpcv0.ServerPoint)
}
