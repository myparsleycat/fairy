//go:build windows

// Command shadergen compiles shaders.hlsl into shaders.bin. Run it with
// go generate in internal/render. It imports the render package, which embeds
// shaders.bin, so shaders.bin must exist to build it.
package main

import (
	"log"
	"os"

	"github.com/myparsleycat/fairy/internal/render"
)

func main() {
	src, err := os.ReadFile("shaders.hlsl")
	if err != nil {
		log.Fatal(err)
	}
	bin, err := render.CompileShaders(src)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("shaders.bin", bin, 0o644); err != nil {
		log.Fatal(err)
	}
}
