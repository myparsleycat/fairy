//go:build !windows && !darwin

package text

// Without ICU the boundary functions fall back to their own rules.

func icuPrevBoundary(string, int) (int, bool) { return 0, false }
func icuNextBoundary(string, int) (int, bool) { return 0, false }
func icuPrevWord(string, int) (int, bool)     { return 0, false }
func icuNextWord(string, int) (int, bool)     { return 0, false }
