package pancursor

import (
	"sync"

	"github.com/myparsleycat/fairy/internal/win"
)

var handles struct {
	sync.Mutex
	m map[[2]int]win.HANDLE
}

// Handle returns the cursor k at size pixels, made on first use and kept for
// the life of the process.
func Handle(k Kind, size int) win.HANDLE {
	handles.Lock()
	defer handles.Unlock()
	key := [2]int{int(k), size}
	if h, ok := handles.m[key]; ok {
		return h
	}
	h := win.CreateCursorFromBGRA(Pixels(k, size), size, size, size/2, size/2)
	if h == 0 {
		h = win.LoadCursor(win.IDC_SIZEALL)
	}
	if handles.m == nil {
		handles.m = map[[2]int]win.HANDLE{}
	}
	handles.m[key] = h
	return h
}
