//go:build !windows && !darwin

package spell

import "context"

// Without a system spell checker nothing is reported misspelled.

func Check(ctx context.Context, lang, text string) []Range { return nil }
func Suggest(lang, word string) []string                   { return nil }
func Add(lang, word string)                                {}
func Ignore(lang, word string)                             {}
