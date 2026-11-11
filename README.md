# Fairy

A native GUI framework for Windows, written in Go without cgo. Direct3D 11 rendering, DirectWrite text, flexbox layout and spring animations, with a declarative API and a themed component kit.

```go
package main

import (
	"fmt"
	"log"

	"github.com/myparsleycat/fairy"
)

func main() {
	count := 0
	err := fairy.Run(fairy.Options{
		Title: "Hello",
		View: func(cx *fairy.Cx) *fairy.El {
			return fairy.Col(
				fairy.Text(fmt.Sprintf("Clicked %d times", count)).FontSize(20),
				fairy.Text("+1").PadXY(14, 6).Radius(6).
					Fill(fairy.Hex(0x3b82f6)).Color(fairy.Hex(0xffffff)).
					OnClick(func() { count++ }),
			).WFull().HFull().Center().Gap(12)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## Getting started

Requires Go 1.27 or later and Windows 10 1703 or later on amd64.

```sh
go get github.com/myparsleycat/fairy
```

Save the example as `main.go` in your Go module and run `go run .`.

## Examples

From this repository:

```sh
go run ./examples/hello
go run ./examples/kit
```

[Guide](docs/README.md) · [Components](kit) · [Examples](examples)

MIT; see [LICENSE](LICENSE) and [third-party notices](NOTICE).
