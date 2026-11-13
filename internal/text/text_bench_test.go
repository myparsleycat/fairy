//go:build windows || darwin

package text

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func BenchmarkRangeConversion(b *testing.B) {
	for _, size := range []int{1024, 16384, 262144} {
		str := strings.Repeat("text 한글 😀 e\u0301 ", size/len("text 한글 😀 e\u0301 ")+1)[:size]
		var ranges [1024][2]int
		rng := rand.New(rand.NewSource(1))
		for i := range ranges {
			ranges[i] = [2]int{rng.Intn(len(str)), rng.Intn(len(str))}
		}
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			l := &Layout{text: str}
			l.utf8ToUTF16(len(str))
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				rg := ranges[i%len(ranges)]
				l.utf8ToUTF16(rg[0])
				l.utf8ToUTF16(rg[1])
				i++
			}
		})
	}
}

func BenchmarkOffsetIndexBuild(b *testing.B) {
	for _, size := range []int{1024, 16384, 262144} {
		str := strings.Repeat("x", size)
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				l := &Layout{text: str}
				l.utf8ToUTF16(len(str))
			}
		})
	}
}
