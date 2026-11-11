package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func imageFixturePath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "images", name)
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	cacheTestFormat  sync.Once
	cacheTestDecodes sync.Map
	cacheTestSeq     atomic.Uint64
)

func cacheTestImage(t *testing.T, decode func() (image.Image, error)) []byte {
	const magic = "fairy-cache-test:"
	cacheTestFormat.Do(func() {
		image.RegisterFormat("fairy-cache-test", magic, func(r io.Reader) (image.Image, error) {
			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			f, ok := cacheTestDecodes.Load(string(data))
			if !ok {
				return nil, errors.New("missing test decoder")
			}
			return f.(func() (image.Image, error))()
		}, func(io.Reader) (image.Config, error) {
			return image.Config{Width: 1, Height: 1}, nil
		})
	})
	data := binary.LittleEndian.AppendUint64([]byte(magic), cacheTestSeq.Add(1))
	key := string(data)
	cacheTestDecodes.Store(key, decode)
	t.Cleanup(func() { cacheTestDecodes.Delete(key) })
	return data
}
