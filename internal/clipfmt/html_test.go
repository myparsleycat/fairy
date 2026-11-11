package clipfmt

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The header's offsets count UTF-8 bytes into the whole data; programs that
// paste HTML cut the fragment by them and silently paste nothing when they
// are off.
func TestCFHTMLOffsets(t *testing.T) {
	frag := "<p><b>굵게</b> and <a href=\"https://example.com/?a=1&amp;b=2\">링크</a></p>"
	data := string(WrapHTML(frag))
	at := func(name string) int {
		m := regexp.MustCompile(name + `:(\d{10})\r\n`).FindStringSubmatch(data)
		if m == nil {
			t.Fatalf("no %s in header:\n%s", name, data)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	sh, eh, sf, ef := at("StartHTML"), at("EndHTML"), at("StartFragment"), at("EndFragment")
	if !strings.HasPrefix(data[sh:], "<html>") || eh != len(data) {
		t.Fatalf("StartHTML %d / EndHTML %d do not frame the document (len %d)", sh, eh, len(data))
	}
	if data[sf:ef] != frag {
		t.Fatalf("fragment by offsets = %q, want %q", data[sf:ef], frag)
	}
	if got, ok := ExtractHTML(append([]byte(data), 0, 0)); !ok || got != frag {
		t.Fatalf("ExtractHTML = %q, %v; want the fragment", got, ok)
	}
}
