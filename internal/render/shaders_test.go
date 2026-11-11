//go:build windows

package render

import (
	"bytes"
	"testing"
)

// A stale shaders.bin still works but makes every window compile HLSL at startup.
func TestShadersBinMatchesSource(t *testing.T) {
	h := sourceHash(shaderSource)
	if len(compiledShaders) < 32 || !bytes.Equal(compiledShaders[:32], h[:]) {
		t.Fatal("shaders.bin is stale; run go generate in internal/render")
	}
}
