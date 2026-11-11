package i18n

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	numfmt "golang.org/x/text/number"
)

// formats holds one locale's CLDR formatting data, built on first use.
type formats struct {
	tag     language.Tag
	printer *message.Printer
	list    listPattern
	// minGroup2 is CLDR minimumGroupingDigits 2: four-digit integers are not
	// grouped (1234, but 12.345). x/text does not apply it.
	minGroup2 bool
	// narrowGroup replaces the no-break space separator of x/text's CLDR data
	// with the narrow no-break space current CLDR uses.
	narrowGroup bool
}

var formatsCache sync.Map

func formatsFor(lang string) *formats {
	if f, ok := formatsCache.Load(lang); ok {
		return f.(*formats)
	}
	tag := language.Make(strings.ReplaceAll(lang, "_", "-"))
	f := &formats{tag: tag, printer: message.NewPrinter(tag), list: listFor(tag)}
	for t := tag; ; t = t.Parent() {
		s := t.String()
		if s == "es-419" || s == "fr-CA" {
			break
		}
		switch s {
		case "es", "pl", "pt-PT":
			f.minGroup2 = true
		case "fr":
			f.narrowGroup = true
		}
		if t.IsRoot() {
			break
		}
	}
	v, _ := formatsCache.LoadOrStore(lang, f)
	return v.(*formats)
}

// pluralForm returns the CLDR cardinal or ordinal category of n in lang as the
// suffix i18next keys use: zero, one, two, few, many or other. A fraction
// counts its visible digits, so 1.5 is "other" in English and "one" in French.
func pluralForm(lang string, n float64, ordinal bool) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "other"
	}
	intPart, frac, _ := strings.Cut(strconv.FormatFloat(math.Abs(n), 'f', -1, 64), ".")
	rules := plural.Cardinal
	if ordinal {
		rules = plural.Ordinal
	}
	i, f := pluralOperand(intPart, 6), pluralOperand(frac, 2)
	switch rules.MatchPlural(formatsFor(lang).tag, i, len(frac), len(frac), f, f) {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	}
	return "other"
}

// pluralOperand returns the decimal digits ds as an int for x/text's plural
// matching, which looks at nothing beyond the value mod 10^width and whether
// it is at least 10^width. A larger value keeps its low digits above that
// bound, where x/text's MatchDigits would collapse it to 10^width.
func pluralOperand(ds string, width int) int {
	ds = strings.TrimLeft(ds, "0")
	big := 0
	if len(ds) > width {
		big = int(math.Pow10(width))
		ds = ds[len(ds)-width:]
	}
	n, _ := strconv.Atoi(ds)
	return big + n
}

func (f *formats) integer(n int64) string {
	return f.text(strconv.FormatInt(n, 10))
}

// decimal rounds n half away from zero to at most maxFrac fraction digits and
// drops trailing zeros, as Intl.NumberFormat with maximumFractionDigits does.
// minFrac pads the fraction with zeros up to that many digits.
func (f *formats) decimal(n float64, maxFrac, minFrac int) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return f.printer.Sprint(numfmt.Decimal(n))
	}
	maxFrac = max(maxFrac, 0)
	minFrac = min(max(minFrac, 0), maxFrac)
	s := roundHalfAway(strconv.FormatFloat(math.Abs(n), 'f', -1, 64), maxFrac)
	if math.Signbit(n) {
		s = "-" + s
	}
	return f.text(s, numfmt.MinFractionDigits(minFrac))
}

// text formats the decimal text s, which already has its final digits, with
// the locale's symbols and grouping.
func (f *formats) text(s string, opts ...numfmt.Option) string {
	intPart, frac, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	opts = append(opts, numfmt.MaxFractionDigits(len(frac)))
	if f.minGroup2 && len(intPart) == 4 {
		opts = append(opts, numfmt.NoSeparator())
	}
	var v any
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		v = i
	} else if x, err := strconv.ParseFloat(s, 64); err == nil {
		v = x
	} else {
		return s
	}
	out := f.printer.Sprint(numfmt.Decimal(v, opts...))
	if f.narrowGroup {
		out = strings.ReplaceAll(out, " ", " ")
	}
	return out
}

// roundHalfAway rounds the non-negative decimal text s to maxFrac fraction
// digits, half away from zero, and trims trailing fraction zeros.
func roundHalfAway(s string, maxFrac int) string {
	intPart, frac, _ := strings.Cut(s, ".")
	if len(frac) > maxFrac {
		up := frac[maxFrac] >= '5'
		b := []byte(intPart + frac[:maxFrac])
		if up {
			i := len(b) - 1
			for ; i >= 0 && b[i] == '9'; i-- {
				b[i] = '0'
			}
			if i >= 0 {
				b[i]++
			} else {
				b = append([]byte{'1'}, b...)
			}
		}
		cut := len(b) - maxFrac
		intPart, frac = string(b[:cut]), string(b[cut:])
	}
	frac = strings.TrimRight(frac, "0")
	if frac == "" {
		return intPart
	}
	return intPart + "." + frac
}

// listPattern is a CLDR "standard" (and, wide) list pattern. Every part has
// the form "{0}<sep>{1}", so only the separators are stored.
type listPattern struct {
	start, middle, end, pair string
	// spanishE turns " y " into " e " before a word starting with an /i/
	// sound, as ICU does for Spanish.
	spanishE bool
}

var listPatterns = map[string]listPattern{
	"und":     {start: ", ", middle: ", ", end: ", ", pair: ", "},
	"en":      {start: ", ", middle: ", ", end: ", and ", pair: " and "},
	"en-001":  {start: ", ", middle: ", ", end: " and ", pair: " and "},
	"de":      {start: ", ", middle: ", ", end: " und ", pair: " und "},
	"fr":      {start: ", ", middle: ", ", end: " et ", pair: " et "},
	"es":      {start: ", ", middle: ", ", end: " y ", pair: " y ", spanishE: true},
	"pt":      {start: ", ", middle: ", ", end: " e ", pair: " e "},
	"it":      {start: ", ", middle: ", ", end: " e ", pair: " e "},
	"nl":      {start: ", ", middle: ", ", end: " en ", pair: " en "},
	"ru":      {start: ", ", middle: ", ", end: " и ", pair: " и "},
	"uk":      {start: ", ", middle: ", ", end: " і ", pair: " і "},
	"pl":      {start: ", ", middle: ", ", end: " i ", pair: " i "},
	"tr":      {start: ", ", middle: ", ", end: " ve ", pair: " ve "},
	"ar":      {start: " و", middle: " و", end: " و", pair: " و"},
	"ko":      {start: ", ", middle: ", ", end: " 및 ", pair: " 및 "},
	"ja":      {start: "、", middle: "、", end: "、", pair: "、"},
	"zh":      {start: "、", middle: "、", end: "和", pair: "和"},
	"zh-Hant": {start: "、", middle: "、", end: "和", pair: "和"},
}

// listFor walks the CLDR parent chain (en-GB → en-001 → en) to the nearest
// locale in listPatterns.
func listFor(tag language.Tag) listPattern {
	for t := tag; !t.IsRoot(); t = t.Parent() {
		if p, ok := listPatterns[t.String()]; ok {
			return p
		}
	}
	return listPatterns["und"]
}

func (p listPattern) join(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + p.last(p.pair, items[1]) + items[1]
	}
	n := len(items)
	out := items[n-2] + p.last(p.end, items[n-1]) + items[n-1]
	for i := n - 3; i >= 1; i-- {
		out = items[i] + p.middle + out
	}
	return items[0] + p.start + out
}

func (p listPattern) last(sep, next string) string {
	if !p.spanishE || sep != " y " {
		return sep
	}
	w := strings.ToLower(next)
	if strings.HasPrefix(w, "i") || strings.HasPrefix(w, "hi") && !strings.HasPrefix(w, "hia") && !strings.HasPrefix(w, "hie") {
		return " e "
	}
	return sep
}
