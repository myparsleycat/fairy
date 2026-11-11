package i18n

import (
	"strings"
	"testing"
)

func TestPluralRules(t *testing.T) {
	for _, c := range []struct {
		lang    string
		n       float64
		ordinal bool
		want    string
	}{
		{"ru", 1, false, "one"},
		{"ru", 2, false, "few"},
		{"ru", 5, false, "many"},
		{"ru", 11, false, "many"},
		{"ru", 21, false, "one"},
		{"ru", 22, false, "few"},
		{"ru", 1.5, false, "other"},
		{"ar", 0, false, "zero"},
		{"ar", 1, false, "one"},
		{"ar", 2, false, "two"},
		{"ar", 3, false, "few"},
		{"ar", 11, false, "many"},
		{"ar", 100, false, "other"},
		{"pl", 1, false, "one"},
		{"pl", 2, false, "few"},
		{"pl", 5, false, "many"},
		{"pl", 12, false, "many"},
		{"pl", 22, false, "few"},
		{"en", 1.5, false, "other"},
		{"fr", 1.5, false, "one"},
		{"en", -1, false, "one"},
		{"en", 1, true, "one"},
		{"en", 2, true, "two"},
		{"en", 3, true, "few"},
		{"en", 4, true, "other"},
		{"en", 11, true, "other"},
		{"en", 22, true, "two"},
		{"en", 113, true, "other"},
		{"ko", 1, true, "other"},
		{"ru", 1000001, false, "one"},
		{"en", 1000002, true, "two"},
		{"sr", 0.123, false, "few"},
	} {
		if got := pluralForm(c.lang, c.n, c.ordinal); got != c.want {
			t.Errorf("pluralForm(%s, %v, ordinal=%v) = %s, want %s", c.lang, c.n, c.ordinal, got, c.want)
		}
	}
}

func TestCatalogPlurals(t *testing.T) {
	c := New("en")
	c.Load("en", map[string]string{
		"place_ordinal_one":   "{{count}}st",
		"place_ordinal_two":   "{{count}}nd",
		"place_ordinal_few":   "{{count}}rd",
		"place_ordinal_other": "{{count}}th",
	})
	c.Load("ru", map[string]string{
		"files_one":    "{{count}} файл",
		"files_few":    "{{count}} файла",
		"files_many":   "{{count}} файлов",
		"apples_one":   "{{count}} яблоко",
		"apples_other": "{{count}} яблок", // no _few: falls back to _other
	})
	for _, step := range []struct {
		lang, key string
		args      []any
		want      string
	}{
		{"ru", "files", []any{"count", 1}, "1 файл"},
		{"ru", "files", []any{"count", 3}, "3 файла"},
		{"ru", "files", []any{"count", 25}, "25 файлов"},
		{"ru", "apples", []any{"count", 3}, "3 яблок"},
		{"en", "place", []any{"count", 1, "ordinal", true}, "1st"},
		{"en", "place", []any{"count", 22, "ordinal", true}, "22nd"},
		{"en", "place", []any{"count", 13, "ordinal", true}, "13th"},
		{"ru", "place", []any{"count", 3, "ordinal", true}, "3rd"}, // English fallback, English ordinal rule
	} {
		c.SetLanguage(step.lang)
		if got := c.T(step.key, step.args...); got != step.want {
			t.Errorf("[%s] T(%q, %v) = %q, want %q", step.lang, step.key, step.args, got, step.want)
		}
	}
}

func TestNumbers(t *testing.T) {
	c := New("en")
	for _, step := range []struct {
		lang string
		got  func() string
		want string
	}{
		{"en", func() string { return c.Integer(1234567) }, "1,234,567"},
		{"de", func() string { return c.Integer(1234567) }, "1.234.567"},
		{"ko", func() string { return c.Integer(-1234567) }, "-1,234,567"},
		{"en", func() string { return c.Integer(9223372036854775807) }, "9,223,372,036,854,775,807"},
		{"en", func() string { return c.Decimal(2.675, 2) }, "2.68"}, // shortest decimal, half away from zero
		{"en", func() string { return c.Decimal(0.125, 2) }, "0.13"},
		{"en", func() string { return c.Decimal(-0.125, 2) }, "-0.13"},
		{"en", func() string { return c.Decimal(9.995, 2) }, "10"},
		{"en", func() string { return c.Decimal(1.5, 2) }, "1.5"},
		{"en", func() string { return c.Decimal(1234.5678, 0) }, "1,235"},
		{"de", func() string { return c.Decimal(-1234.5, 2) }, "-1.234,5"},
		{"ko", func() string { return c.Decimal(1234.5678, 2) }, "1,234.57"},
		{"es", func() string { return c.Integer(1234) }, "1234"}, // minimumGroupingDigits 2
		{"es", func() string { return c.Integer(12345) }, "12.345"},
		{"fr", func() string { return c.Integer(1234567) }, "1 234 567"},
	} {
		c.SetLanguage(step.lang)
		if got := step.got(); got != step.want {
			t.Errorf("[%s] got %q, want %q", step.lang, got, step.want)
		}
	}
}

func TestNumberFormatter(t *testing.T) {
	c := New("en")
	c.Load("en", map[string]string{
		"size":  "{{n, number}} MB",
		"ratio": "{{n, number(maximumFractionDigits: 1)}}",
		"fixed": "{{n, number(minimumFractionDigits: 2)}}",
	})
	for _, step := range []struct {
		lang, key string
		n         any
		want      string
	}{
		{"en", "size", 1234.5678, "1,234.568 MB"},
		{"en", "size", 1234567, "1,234,567 MB"},
		{"de", "size", 1234.5678, "1.234,568 MB"}, // English message, German number
		{"en", "ratio", 0.25, "0.3"},
		{"en", "fixed", 3, "3.00"},
		{"en", "size", "12.5", "12.5 MB"},
		{"en", "size", "n/a", "n/a MB"},
	} {
		c.SetLanguage(step.lang)
		if got := c.T(step.key, "n", step.n); got != step.want {
			t.Errorf("[%s] T(%q, n=%v) = %q, want %q", step.lang, step.key, step.n, got, step.want)
		}
	}
}

func TestList(t *testing.T) {
	c := New("en")
	abc := []string{"a", "b", "c"}
	for _, step := range []struct {
		lang  string
		items []string
		want  string
	}{
		{"en", nil, ""},
		{"en", []string{"a"}, "a"},
		{"en", []string{"a", "b"}, "a and b"},
		{"en", abc, "a, b, and c"},
		{"en", []string{"a", "b", "c", "d"}, "a, b, c, and d"},
		{"en-GB", abc, "a, b and c"},
		{"de", abc, "a, b und c"},
		{"de-AT", abc, "a, b und c"},
		{"ko", abc, "a, b 및 c"},
		{"ja", abc, "a、b、c"},
		{"zh-TW", abc, "a、b和c"},
		{"es", []string{"padre", "hijo"}, "padre e hijo"},
		{"es", []string{"España", "Italia"}, "España e Italia"},
		{"es", []string{"agua", "hielo"}, "agua y hielo"}, // hie- is a diphthong
		{"xx", abc, "a, b, c"},
	} {
		c.SetLanguage(step.lang)
		if got := c.List(step.items); got != step.want {
			t.Errorf("[%s] List(%q) = %q, want %q", step.lang, step.items, got, step.want)
		}
	}
}

func TestNegotiate(t *testing.T) {
	for _, step := range []struct {
		preferred, supported []string
		want                 string
	}{
		{[]string{"de-AT"}, []string{"en", "de"}, "de"},
		{[]string{"zh-TW"}, []string{"en", "zh-Hans", "zh-Hant"}, "zh-Hant"},
		{[]string{"zh-CN"}, []string{"en", "zh-Hant", "zh-Hans"}, "zh-Hans"},
		{[]string{"zh-TW"}, []string{"en", "zh"}, "en"}, // zh is Hans
		{[]string{"pa-PK"}, []string{"en", "pa", "pa-Arab"}, "pa-Arab"},
		{[]string{"pa-IN"}, []string{"en", "pa-Arab"}, "en"},
		{[]string{"nb-NO"}, []string{"en", "no"}, "no"},
		{[]string{"no"}, []string{"en", "nn"}, "nn"},
		{[]string{"fr-CA", "de"}, []string{"en", "de", "fr"}, "fr"},
		{[]string{"ja", "de_DE"}, []string{"en", "de"}, "de"},
		{[]string{"not a tag!", "ko-KR"}, []string{"en", "ko"}, "ko"},
		{[]string{"it"}, []string{"en", "de"}, "en"},
		{[]string{"it"}, nil, ""},
	} {
		if got := Negotiate(step.preferred, step.supported); got != step.want {
			t.Errorf("Negotiate(%q, %q) = %q, want %q", step.preferred, step.supported, got, step.want)
		}
	}

	c := New("en")
	c.Load("zh-Hans", map[string]string{"k": "简"})
	c.Load("zh-Hant", map[string]string{"k": "繁"})
	if got := c.Negotiate([]string{"zh-HK"}); got != "zh-Hant" || c.T("k") != "繁" {
		t.Errorf("Catalog.Negotiate(zh-HK) = %q, T = %q", got, c.T("k"))
	}
	if got := c.Negotiate([]string{"ko"}); got != "en" {
		t.Errorf("Catalog.Negotiate(ko) = %q, want the fallback en", got)
	}
}

func TestPosixLanguages(t *testing.T) {
	env := map[string]string{"LANGUAGE": "de_AT:fr", "LC_ALL": "C", "LANG": "de_AT.UTF-8@euro"}
	got := posixLanguages(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if strings.Join(got, ",") != "de-AT,fr" {
		t.Errorf("posixLanguages = %q", got)
	}
}
