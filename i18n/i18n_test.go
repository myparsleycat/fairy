package i18n

import (
	"testing"
	"time"
)

func TestJosa(t *testing.T) {
	for _, c := range []struct{ word, pair, want string }{
		{"사과", "을/를", "사과를"},
		{"책", "을/를", "책을"},
		{"모드", "은/는", "모드는"},
		{"파일", "이/가", "파일이"},
		{"친구", "와/과", "친구와"},
		{"동생", "와/과", "동생과"},
		{"서울", "으로/로", "서울로"}, // '로' follows a final ㄹ.
		{"집", "으로/로", "집으로"},
		{"바다", "으로/로", "바다로"},
		{"config.json", "은/는", "config.json는"}, // A non-Hangul letter counts as having no final consonant.
	} {
		if got := Josa(c.word, c.pair); got != c.want {
			t.Errorf("Josa(%q, %q) = %q, want %q", c.word, c.pair, got, c.want)
		}
	}
}

func TestCatalog(t *testing.T) {
	c := New("en")
	if err := c.LoadJSON("en", []byte(`{
		"files_one": "{{count}} file",
		"files_other": "{{count}} files",
		"items_zero": "No items",
		"items_other": "{{count}} items",
		"mods": {"delete": {"title": "Delete {{name}}?"}},
		"onlyEnglish": "fallback"
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadJSON("ko", []byte(`{
		"files_other": "파일 {{count}}개",
		"mods": {"delete": {"title": "{{name, josa(pair: 을/를)}} 삭제할까요?"}},
		"move": "{{to, josa(pair: 으로/로)}} 이동"
	}`)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		lang, key string
		args      []any
		want      string
	}{
		{"en", "files", []any{"count", 1}, "1 file"},
		{"en", "files", []any{"count", 2}, "2 files"},
		{"en", "items", []any{"count", 0}, "No items"},
		{"en", "items", []any{"count", 3}, "3 items"},
		{"en", "mods.delete.title", []any{"name", "Foo"}, "Delete Foo?"},
		{"ko", "files", []any{"count", 1}, "파일 1개"}, // Korean has no "one" category.
		{"ko-KR", "mods.delete.title", []any{"name", "모드"}, "모드를 삭제할까요?"},
		{"ko", "move", []any{"to", "폴더"}, "폴더로 이동"},
		{"ko", "onlyEnglish", nil, "fallback"},
		{"ko", "missing.key", nil, "missing.key"},
		{"en", "mods.delete.title", nil, "Delete {{name}}?"},
	} {
		c.SetLanguage(step.lang)
		if got := c.T(step.key, step.args...); got != step.want {
			t.Errorf("[%s] T(%q, %v) = %q, want %q", step.lang, step.key, step.args, got, step.want)
		}
	}
}

func TestCatalogFormatterCanUpdateCatalog(t *testing.T) {
	c := New("en")
	c.Load("en", map[string]string{"outer": "{{key, translate}}"})
	c.Load("ko", map[string]string{"inner": "translated"})
	c.Format("translate", func(value any, _ string, _ map[string]string) string {
		c.SetLanguage("ko")
		return c.T(value.(string))
	})

	result := make(chan string, 1)
	go func() { result <- c.T("outer", "key", "inner") }()
	select {
	case got := <-result:
		if got != "translated" {
			t.Errorf("T returned %q, want translated", got)
		}
	case <-time.After(time.Second):
		t.Fatal("T blocked while the formatter updated the catalog")
	}
}
