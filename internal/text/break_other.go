//go:build !windows

package text

// Without ICU the boundary functions fall back to their own rules.

func icuPrevBoundary(s string, b int) (int, bool) { return 0, false }
func icuNextBoundary(s string, b int) (int, bool) { return 0, false }
func icuPrevWord(s string, b int) (int, bool)     { return 0, false }
func icuNextWord(s string, b int) (int, bool)     { return 0, false }
