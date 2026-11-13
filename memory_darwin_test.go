package fairy

import "testing"

func TestTrimImageMemoryReleasesTexturesBeforeReturning(t *testing.T) {
	w := &Window{
		frame: 2,
		textures: map[uint64]*texEntry{
			1: {frame: 0},
			2: {frame: 1},
		},
	}
	liveMu.Lock()
	live[w] = struct{}{}
	liveMu.Unlock()
	defer func() {
		liveMu.Lock()
		delete(live, w)
		liveMu.Unlock()
	}()
	watchMemoryPressure()
	trimImageMemory()
	if _, ok := w.textures[1]; ok {
		t.Error("trimImageMemory returned before releasing an idle texture")
	}
	if _, ok := w.textures[2]; !ok {
		t.Error("trimImageMemory released a texture drawn in the last frame")
	}
}
