//go:build windows

package uia

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/internal/com"
)

var pSysStringLen = oleaut.NewProc("SysStringLen")

func TestAllocBSTRKeepsEmbeddedNUL(t *testing.T) {
	b := allocBSTR("a\x00b")
	if b == 0 {
		t.Fatal("SysAllocStringLen failed")
	}
	defer pSysFreeString.Call(b)
	n, _, _ := pSysStringLen.Call(b)
	if got := unsafe.Slice((*uint16)(com.Ptr(b)), n); !slices.Equal(got, []uint16{'a', 0, 'b'}) {
		t.Fatalf("BSTR = %v, want a, NUL, b", got)
	}
}
