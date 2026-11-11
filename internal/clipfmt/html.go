package clipfmt

import (
	"fmt"
	"strconv"
	"strings"
)

const cfHTMLHeader = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"

// WrapHTML wraps an HTML fragment in the clipboard's "HTML Format": a header
// of byte offsets into the UTF-8 data, then a document holding the fragment
// between its markers.
func WrapHTML(fragment string) []byte {
	const pre = "<html><body>\r\n<!--StartFragment-->"
	const post = "<!--EndFragment-->\r\n</body></html>"
	startHTML := len(fmt.Sprintf(cfHTMLHeader, 0, 0, 0, 0))
	startFrag := startHTML + len(pre)
	endFrag := startFrag + len(fragment)
	endHTML := endFrag + len(post)
	return []byte(fmt.Sprintf(cfHTMLHeader, startHTML, endHTML, startFrag, endFrag) + pre + fragment + post)
}

// ExtractHTML returns the fragment of "HTML Format" clipboard data.
func ExtractHTML(data []byte) (string, bool) {
	s := strings.TrimRight(string(data), "\x00")
	offset := func(name string) int {
		i := strings.Index(s, name+":")
		if i < 0 {
			return -1
		}
		v := s[i+len(name)+1:]
		if j := strings.IndexAny(v, "\r\n"); j >= 0 {
			v = v[:j]
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return -1
		}
		return n
	}
	a, b := offset("StartFragment"), offset("EndFragment")
	if a < 0 || b < a || b > len(s) {
		return "", false
	}
	return s[a:b], true
}
