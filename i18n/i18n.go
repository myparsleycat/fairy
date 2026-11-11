// Package i18n is a message catalog that reads i18next-style JSON resources:
// nested keys joined with ".", {{name}} interpolation, formatters written as
// {{name, josa(pair: 을/를)}} or {{n, number}}, and plural keys selected by a
// "count" argument with CLDR plural rules (_zero, _one, _two, _few, _many,
// _other, and _ordinal_one and so on when "ordinal" is true).
//
// Numbers and lists are formatted with CLDR data per locale, and Negotiate
// matches the user's preferred languages (SystemLanguages) to loaded ones.
//
// A Catalog is safe for concurrent use.
package i18n

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Formatter turns an interpolated value into text. lang is the selected
// language, also when the message comes from a fallback language, as in
// i18next. opts holds the options written after the formatter name, such as
// pair for josa(pair: 은/는).
type Formatter func(value any, lang string, opts map[string]string) string

// Catalog holds messages per language.
type Catalog struct {
	mu       sync.RWMutex
	msgs     map[string]map[string]string
	lang     string
	fallback string
	formats  map[string]Formatter
}

// New returns an empty catalog that falls back to the fallback language for
// keys the current language lacks. The josa and number formatters are
// registered.
func New(fallback string) *Catalog {
	c := &Catalog{msgs: map[string]map[string]string{}, lang: fallback, fallback: fallback, formats: map[string]Formatter{}}
	c.formats["josa"] = josaFormatter
	c.formats["number"] = numberFormatter
	return c
}

// Load adds messages for lang, replacing keys that already exist.
func (c *Catalog) Load(lang string, msgs map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.msgs[lang]
	if m == nil {
		m = map[string]string{}
		c.msgs[lang] = m
	}
	for k, v := range msgs {
		m[k] = v
	}
}

// LoadJSON adds messages for lang from a JSON object. Nested objects become
// keys joined with "."; numbers and booleans are kept as text.
func (c *Catalog) LoadJSON(lang string, data []byte) error {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("i18n: %s: %w", lang, err)
	}
	flat := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if prefix != "" {
					k = prefix + "." + k
				}
				walk(k, x)
			}
		case string:
			flat[prefix] = v
		case nil:
		default:
			flat[prefix] = fmt.Sprint(v)
		}
	}
	walk("", root)
	c.Load(lang, flat)
	return nil
}

// SetLanguage selects the language T uses. A regional tag such as "ko-KR"
// falls back to "ko" when only that is loaded.
func (c *Catalog) SetLanguage(lang string) {
	c.mu.Lock()
	c.lang = lang
	c.mu.Unlock()
}

// Language returns the selected language.
func (c *Catalog) Language() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lang
}

// Languages returns the loaded languages, sorted.
func (c *Catalog) Languages() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string
	for l := range c.msgs {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// Format registers a formatter for {{value, name(...)}}.
func (c *Catalog) Format(name string, f Formatter) {
	c.mu.Lock()
	// Interpolation can still be using the previous map outside the lock.
	c.formats = maps.Clone(c.formats)
	c.formats[name] = f
	c.mu.Unlock()
}

// chain lists the languages to search, most specific first.
func (c *Catalog) chain() []string {
	out := []string{c.lang}
	if i := strings.IndexAny(c.lang, "-_"); i > 0 {
		out = append(out, c.lang[:i])
	}
	if c.fallback != c.lang {
		out = append(out, c.fallback)
	}
	return out
}

func (c *Catalog) lookup(key string) (string, string, bool) {
	for _, l := range c.chain() {
		if s, ok := c.msgs[l][key]; ok {
			return s, l, true
		}
	}
	return "", "", false
}

// Has reports whether key exists in the selected or fallback language.
func (c *Catalog) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, _, ok := c.lookup(key)
	return ok
}

// T returns the message for key with args interpolated. args are name/value
// pairs: T("files", "count", 3, "name", n). A numeric "count" selects the key
// with the CLDR plural suffix of each searched language: key_zero when count
// is 0 and that key exists, then key_<category>, key_other and key. With
// "ordinal", true the ordinal category picks key_ordinal_<category>, then
// key_ordinal_other and key. A missing key returns the key itself.
func (c *Catalog) T(key string, args ...any) string {
	vars := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			vars[name] = args[i+1]
		}
	}
	c.mu.RLock()
	msg, ok := "", false
	if n, isNum := number(vars["count"]); isNum {
		ordinal, _ := vars["ordinal"].(bool)
		for _, l := range c.chain() {
			if msg, ok = c.pluralMessage(l, key, n, ordinal); ok {
				break
			}
		}
	} else {
		msg, _, ok = c.lookup(key)
	}
	lang, formats := c.lang, c.formats
	c.mu.RUnlock()
	if !ok {
		return key
	}
	return interpolate(msg, lang, vars, formats)
}

func (c *Catalog) pluralMessage(lang, key string, n float64, ordinal bool) (string, bool) {
	m := c.msgs[lang]
	if m == nil {
		return "", false
	}
	if n == 0 && !ordinal {
		if s, ok := m[key+"_zero"]; ok {
			return s, true
		}
	}
	prefix := key + "_"
	if ordinal {
		prefix += "ordinal_"
	}
	for _, k := range [...]string{prefix + pluralForm(lang, n, ordinal), prefix + "other", key} {
		if s, ok := m[k]; ok {
			return s, true
		}
	}
	return "", false
}

// PluralCategory returns the CLDR cardinal category of n in the selected
// language: zero, one, two, few, many or other.
func (c *Catalog) PluralCategory(n float64) string {
	return pluralForm(c.Language(), n, false)
}

// OrdinalCategory returns the CLDR ordinal category of n in the selected
// language, such as "two" for 22 in English (22nd).
func (c *Catalog) OrdinalCategory(n float64) string {
	return pluralForm(c.Language(), n, true)
}

// Integer formats n with the selected language's digits and grouping, as
// Intl.NumberFormat shows an integer: 1,234,567 in English, 1.234.567 in
// German.
func (c *Catalog) Integer(n int64) string {
	return formatsFor(c.Language()).integer(n)
}

// Decimal formats n rounded half away from zero to at most maxFrac fraction
// digits, trailing zeros dropped, as Intl.NumberFormat with
// maximumFractionDigits shows it: Decimal(1234.5, 2) is 1,234.5 in English and
// 1.234,5 in German.
func (c *Catalog) Decimal(n float64, maxFrac int) string {
	return formatsFor(c.Language()).decimal(n, maxFrac, 0)
}

// List joins items as the selected language's conjunction list: "a, b, and c"
// in English, "a, b und c" in German, "a, b 및 c" in Korean.
func (c *Catalog) List(items []string) string {
	return formatsFor(c.Language()).list.join(items)
}

// UseSystemLanguage selects the loaded language that best matches
// SystemLanguages, or the fallback language when none does, and returns it.
func (c *Catalog) UseSystemLanguage() string {
	return c.Negotiate(SystemLanguages())
}

// Negotiate selects the loaded language that best matches preferred, as the
// package-level Negotiate decides, or the fallback language when none does,
// and returns it. The fallback language wins ties.
func (c *Catalog) Negotiate(preferred []string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	supported := []string{c.fallback}
	for l := range c.msgs {
		if l != c.fallback {
			supported = append(supported, l)
		}
	}
	slices.Sort(supported[1:])
	c.lang = Negotiate(preferred, supported)
	return c.lang
}

func interpolate(msg, lang string, vars map[string]any, formats map[string]Formatter) string {
	if !strings.Contains(msg, "{{") {
		return msg
	}
	var b strings.Builder
	for {
		i := strings.Index(msg, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(msg[i+2:], "}}")
		if j < 0 {
			break
		}
		b.WriteString(msg[:i])
		b.WriteString(expand(strings.TrimSpace(msg[i+2:i+2+j]), lang, vars, formats))
		msg = msg[i+2+j+2:]
	}
	b.WriteString(msg)
	return b.String()
}

// expand resolves "name" or "name, formatter(opt: value; opt2: value)".
func expand(expr, lang string, vars map[string]any, formats map[string]Formatter) string {
	name, format, _ := strings.Cut(expr, ",")
	name = strings.TrimSpace(name)
	v, ok := vars[name]
	if !ok {
		return "{{" + expr + "}}"
	}
	format = strings.TrimSpace(format)
	if format == "" {
		return fmt.Sprint(v)
	}
	fname, rest, _ := strings.Cut(format, "(")
	fname = strings.TrimSpace(fname)
	f := formats[fname]
	if f == nil {
		return fmt.Sprint(v)
	}
	opts := map[string]string{}
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ")")
	for _, kv := range strings.Split(rest, ";") {
		if k, val, ok := strings.Cut(kv, ":"); ok {
			opts[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	return f(v, lang, opts)
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// numberFormatter is i18next's number formatter: {{n, number}} shows n as
// Intl.NumberFormat does, with at most 3 fraction digits unless
// maximumFractionDigits or minimumFractionDigits says otherwise. A string that
// parses as a number is formatted too.
func numberFormatter(v any, lang string, opts map[string]string) string {
	n, ok := number(v)
	if !ok {
		s, isString := v.(string)
		x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if !isString || err != nil {
			return fmt.Sprint(v)
		}
		n = x
	}
	minFrac, _ := strconv.Atoi(opts["minimumFractionDigits"])
	maxFrac, err := strconv.Atoi(opts["maximumFractionDigits"])
	if err != nil {
		maxFrac = max(3, minFrac)
	}
	f := formatsFor(lang)
	if minFrac == 0 {
		switch i := v.(type) {
		case int:
			return f.integer(int64(i))
		case int64:
			return f.integer(i)
		}
	}
	return f.decimal(n, maxFrac, minFrac)
}

// Default is the package-level catalog, falling back to English.
var Default = New("en")

// T looks up key in Default.
func T(key string, args ...any) string { return Default.T(key, args...) }
