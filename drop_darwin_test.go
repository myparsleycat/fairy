package fairy

import (
	"slices"
	"sync"
	"testing"

	"github.com/ebitengine/purego/objc"
	"golang.org/x/text/unicode/norm"

	"github.com/myparsleycat/fairy/internal/darwin"
)

// testDragPasteboard is the pasteboard of the drags testDragInfoClass makes.
var testDragPasteboard darwin.ID

// testDragInfoClass is an NSDraggingInfo whose pasteboard is
// testDragPasteboard. It is registered once: the runtime keeps classes for
// the life of the process, and tests may run more than once.
var testDragInfoClass = sync.OnceValue(func() darwin.Class {
	cls, err := objc.RegisterClass("FairyTestDragInfo", darwin.Cls("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: darwin.Sel("draggingPasteboard"), Fn: func(darwin.ID, darwin.SEL) darwin.ID { return testDragPasteboard }},
	})
	if err != nil {
		panic(err)
	}
	return cls
})

// The view takes file URLs, and a drag's pasteboard reads as file paths.
func TestDarwinDraggedPaths(t *testing.T) {
	w := newTestWindow(t, func() *El { return Box().Size(100, 100) })
	pb := darwin.ID(0)
	var types []string
	Do(func() {
		reg := darwin.Send(w.view, "registeredDraggedTypes")
		for i := range darwin.Get[uint64](reg, "count") {
			types = append(types, darwin.GoString(darwin.Send(reg, "objectAtIndex:", i)))
		}
		pb = darwin.Retain(darwin.SendClass("NSPasteboard", "pasteboardWithUniqueName"))
	})
	if !slices.Contains(types, "public.file-url") {
		t.Fatalf("registered dragged types = %v", types)
	}
	want := []string{"/tmp/a b.txt", "/Users/x/한글.png"}
	var got []string
	Do(func() {
		defer darwin.Send(pb, "releaseGlobally")
		urls := darwin.SendClass("NSMutableArray", "array")
		for _, p := range want {
			darwin.Send(urls, "addObject:", darwin.SendClass("NSURL", "fileURLWithPath:", darwin.String(p)))
		}
		darwin.Send(pb, "clearContents")
		darwin.Send(pb, "writeObjects:", urls)
		testDragPasteboard = pb
		info := darwin.ID(testDragInfoClass()).Send(darwin.Sel("new"))
		// NSURL hands back paths decomposed (NFD), as the file system
		// stores them.
		for _, p := range draggedPaths(info) {
			got = append(got, norm.NFC.String(p))
		}
		darwin.Release(info)
	})
	if !slices.Equal(got, want) {
		t.Errorf("dragged paths = %q, want %q", got, want)
	}
}
