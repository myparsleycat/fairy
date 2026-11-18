// Package fnvhash folds values into a 64-bit FNV-1a hash, for versions and
// cache keys kept in memory.
package fnvhash

import "math"

// Hash is an FNV-1a hash. Its methods return the hash with a value folded in.
type Hash uint64

const prime = 1099511628211

// New returns the hash of no input.
func New() Hash { return 14695981039346656037 }

// Uint folds in v.
func (h Hash) Uint(v uint64) Hash {
	for range 8 {
		h = (h ^ Hash(v&0xff)) * prime
		v >>= 8
	}
	return h
}

// Str folds in s and its length.
func (h Hash) Str(s string) Hash {
	for i := 0; i < len(s); i++ {
		h = (h ^ Hash(s[i])) * prime
	}
	return h.Uint(uint64(len(s)))
}

// Float folds in the bits of f.
func (h Hash) Float(f float32) Hash { return h.Uint(uint64(math.Float32bits(f))) }

// Bool folds in b.
func (h Hash) Bool(b bool) Hash {
	if b {
		return h.Uint(1)
	}
	return h.Uint(0)
}
