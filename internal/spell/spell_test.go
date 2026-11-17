package spell

import (
	"reflect"
	"testing"
)

func TestUTF16ToBytes(t *testing.T) {
	text := "😀 helo wrld"
	got := utf16ToBytes(text, []Range{{3, 7}, {8, 12}})
	want := []Range{{5, 9}, {10, 14}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("utf16ToBytes = %v, want %v", got, want)
	}
}
