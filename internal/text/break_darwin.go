package text

import (
	"sync"

	"github.com/ebitengine/purego"
)

// icu loads the ICU that macOS ships as libicucore, which exports the C API
// without version suffixes. It returns nil when the library cannot be loaded.
var icu = sync.OnceValue(func() *icuAPI {
	lib, err := purego.Dlopen("/usr/lib/libicucore.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil
	}
	for _, name := range []string{"ubrk_open", "ubrk_setText", "ubrk_preceding", "ubrk_following", "ubrk_getRuleStatus"} {
		if _, err := purego.Dlsym(lib, name); err != nil {
			return nil
		}
	}
	p := &icuAPI{}
	purego.RegisterLibFunc(&p.open, lib, "ubrk_open")
	purego.RegisterLibFunc(&p.setText, lib, "ubrk_setText")
	purego.RegisterLibFunc(&p.preceding, lib, "ubrk_preceding")
	purego.RegisterLibFunc(&p.following, lib, "ubrk_following")
	purego.RegisterLibFunc(&p.ruleStatus, lib, "ubrk_getRuleStatus")
	return p
})
