//go:build !windows && !darwin

package i18n

import "os"

// SystemLanguages returns the languages the POSIX locale variables name, most
// wanted first (see posixLanguages).
func SystemLanguages() []string {
	return posixLanguages(os.LookupEnv)
}
