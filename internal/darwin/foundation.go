//go:build darwin

package darwin

import (
	"unicode/utf16"
	"unsafe"
)

// CGFloat is double on 64-bit macOS.
type CGFloat = float64

type CGPoint struct{ X, Y CGFloat }
type CGSize struct{ W, H CGFloat }
type CGRect struct {
	Origin CGPoint
	Size   CGSize
}

// NSRange is a range of UTF-16 code units.
type NSRange struct{ Location, Length uint64 }

// NSNotFound is NSRange.Location for no range.
const NSNotFound = 1<<63 - 1

func Rect(x, y, w, h float64) CGRect { return CGRect{CGPoint{x, y}, CGSize{w, h}} }

// String returns an autoreleased NSString holding s.
func String(s string) ID {
	if s == "" {
		return SendClass("NSString", "string")
	}
	u := utf16.Encode([]rune(s))
	return Send(SendClass("NSString", "alloc"), "initWithCharacters:length:", unsafe.Pointer(&u[0]), uint64(len(u))).
		Send(Sel("autorelease"))
}

// GoString returns the text of an NSString, or "" for nil. A string that
// is not valid UTF-16, such as one with an unpaired surrogate a page or an
// input method made, has U+FFFD in its place.
func GoString(id ID) string {
	if id == 0 {
		return ""
	}
	p := Get[*byte](id, "UTF8String")
	if p == nil {
		// UTF8String fails for the whole string on an unpaired surrogate:
		// decode its UTF-16 instead.
		n := Get[uint64](id, "length")
		if n == 0 {
			return ""
		}
		u := make([]uint16, n)
		Send(id, "getCharacters:range:", unsafe.Pointer(&u[0]), NSRange{Length: n})
		return string(utf16.Decode(u))
	}
	const nsUTF8StringEncoding = 4
	n := Get[uint64](id, "lengthOfBytesUsingEncoding:", uint64(nsUTF8StringEncoding))
	return string(unsafe.Slice(p, n))
}

// Data returns an autoreleased NSData copying b.
func Data(b []byte) ID {
	if len(b) == 0 {
		return SendClass("NSData", "data")
	}
	return SendClass("NSData", "dataWithBytes:length:", unsafe.Pointer(&b[0]), uint64(len(b)))
}

// Bytes copies the contents of an NSData.
func Bytes(data ID) []byte {
	if data == 0 {
		return nil
	}
	n := Get[uint64](data, "length")
	if n == 0 {
		return nil
	}
	p := Get[unsafe.Pointer](data, "bytes")
	return append([]byte(nil), unsafe.Slice((*byte)(p), n)...)
}

// Number returns an NSNumber for v.
func Number(v float64) ID { return SendClass("NSNumber", "numberWithDouble:", v) }
