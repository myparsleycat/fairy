// Package spell checks spelling with the system spell checker.
package spell

// Range identifies a misspelling by UTF-8 byte offsets, with End exclusive.
type Range struct{ Start, End int }
