package syntax

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type tk struct {
	text string
	kind Kind
}

func toks(lang Lang, src string) []tk {
	var out []tk
	for _, t := range Scan(lang, src) {
		out = append(out, tk{src[t.Start:t.End], t.Kind})
	}
	return out
}

func kindOf(lang Lang, src, text string) (Kind, bool) {
	for _, t := range toks(lang, src) {
		if t.text == text {
			return t.kind, true
		}
	}
	return 0, false
}

func count(lang Lang, src, text string, kind Kind) int {
	n := 0
	for _, t := range toks(lang, src) {
		if t.text == text && t.kind == kind {
			n++
		}
	}
	return n
}

func has(t *testing.T, lang Lang, src string, want ...tk) {
	t.Helper()
	got := toks(lang, src)
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("lang %d: no %q as %d in %v", lang, w.text, w.kind, got)
		}
	}
}

func uncolored(t *testing.T, lang Lang, src, text string) {
	t.Helper()
	if k, ok := kindOf(lang, src, text); ok {
		t.Errorf("lang %d: %q colored %d, want plain", lang, text, k)
	}
}

func TestLangFromName(t *testing.T) {
	for name, want := range map[string]Lang{"TypeScript": LangTS, "tsx": LangTSX, "jsx": LangJS, "bash": LangShell, "yml": LangYAML,
		"rs": LangRust, "py": LangPython, "Python3": LangPython, "golang": LangGo, "patch": LangDiff, "svg": LangHTML} {
		if got, ok := LangFromName(name); !ok || got != want {
			t.Errorf("%s: got %d, %v", name, got, ok)
		}
	}
	for _, name := range []string{"brainfuck", "", "text"} {
		if _, ok := LangFromName(name); ok || ScanName(name, "let x = 1") != nil {
			t.Errorf("%q is known", name)
		}
	}
}

// goldens holds one snippet per language and every range its scanner gives,
// in order.
var goldens = []struct {
	lang Lang
	src  string
	want []tk
}{
	{LangRust, "fn main() {\n    let v: Vec<u8> = vec![1, 2]; // two\n    println!(\"{}\", v.len());\n}", []tk{
		{"fn", Keyword}, {"main", Function}, {"()", Punctuation}, {"{", Punctuation}, {"let", Keyword}, {":", Punctuation},
		{"Vec", Type}, {"<", Punctuation}, {"u8", Type}, {">", Punctuation}, {"=", Punctuation}, {"vec!", Function}, {"[", Punctuation},
		{"1", Number}, {",", Punctuation}, {"2", Number}, {"];", Punctuation}, {"// two", Comment}, {"println!", Function},
		{"(", Punctuation}, {"\"{}\"", String}, {",", Punctuation}, {".", Punctuation}, {"len", Function}, {"());", Punctuation},
		{"}", Punctuation},
	}},
	{LangJS, "const xs = [1, 2].map((x) => x * 2); // double\nconsole.log(`n=${xs.length}`, /a+/g);", []tk{
		{"const", Keyword}, {"=", Punctuation}, {"[", Punctuation}, {"1", Number}, {",", Punctuation}, {"2", Number},
		{"].", Punctuation}, {"map", Function}, {"((", Punctuation}, {")", Punctuation}, {"=>", Punctuation}, {"*", Punctuation},
		{"2", Number}, {");", Punctuation}, {"// double", Comment}, {".", Punctuation}, {"log", Function}, {"(", Punctuation},
		{"`n=", String}, {"${", Punctuation}, {".", Punctuation}, {"}", Punctuation}, {"`", String}, {",", Punctuation},
		{"/a+/g", String}, {");", Punctuation},
	}},
	{LangTS, "interface User { id: number; name?: string }\nexport function greet(u: User): string {\n  return `hi ${u.name ?? \"anon\"}`;\n}", []tk{
		{"interface", Keyword}, {"User", Type}, {"{", Punctuation}, {":", Punctuation}, {"number", Type}, {";", Punctuation},
		{"?:", Punctuation}, {"string", Type}, {"}", Punctuation}, {"export", Keyword}, {"function", Keyword}, {"greet", Function},
		{"(", Punctuation}, {"u", Parameter}, {":", Punctuation}, {"User", Type}, {"):", Punctuation}, {"string", Type}, {"{", Punctuation},
		{"return", Keyword}, {"`hi ", String}, {"${", Punctuation}, {".", Punctuation}, {"??", Punctuation}, {"\"anon\"", String},
		{"}", Punctuation}, {"`", String}, {";", Punctuation}, {"}", Punctuation},
	}},
	{LangPython, "@dataclass\nclass Point:\n    x: int = 0  # origin\n    def dist(self, other: \"Point\") -> float:\n        return math.hypot(self.x - other.x, f\"{self.y!r}\")\nprint(None, True, 1e3, r'\\d+', b\"x\")", []tk{
		{"@dataclass", Keyword}, {"class", Keyword}, {"Point", Type}, {":", Punctuation}, {":", Punctuation}, {"int", Type},
		{"=", Punctuation}, {"0", Number}, {"# origin", Comment}, {"def", Keyword}, {"dist", Function}, {"(", Punctuation},
		{"self", Keyword}, {",", Punctuation}, {":", Punctuation}, {"\"Point\"", String}, {")", Punctuation}, {"->", Punctuation},
		{"float", Type}, {":", Punctuation}, {"return", Keyword}, {".", Punctuation}, {"hypot", Function}, {"(", Punctuation},
		{"self", Keyword}, {".", Punctuation}, {"-", Punctuation}, {".", Punctuation}, {",", Punctuation}, {"f\"", String},
		{"{", Punctuation}, {"self", Keyword}, {".", Punctuation}, {"!", Punctuation}, {"}", Punctuation}, {"\"", String},
		{")", Punctuation}, {"print", Function}, {"(", Punctuation}, {"None", Number}, {",", Punctuation}, {"True", Number},
		{",", Punctuation}, {"1e3", Number}, {",", Punctuation}, {"r'\\d+'", String}, {",", Punctuation}, {"b\"x\"", String},
		{")", Punctuation},
	}},
	{LangGo, "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfor i := 0; i < 3; i++ {\n\t\tfmt.Println(`raw\\n`, 'x', nil) // done\n\t}\n}", []tk{
		{"package", Keyword}, {"import", Keyword}, {"\"fmt\"", String}, {"func", Keyword}, {"main", Function}, {"()", Punctuation},
		{"{", Punctuation}, {"for", Keyword}, {":=", Punctuation}, {"0", Number}, {";", Punctuation}, {"<", Punctuation}, {"3", Number},
		{";", Punctuation}, {"++", Punctuation}, {"{", Punctuation}, {".", Punctuation}, {"Println", Function}, {"(", Punctuation},
		{"`raw\\n`", String}, {",", Punctuation}, {"'x'", String}, {",", Punctuation}, {"nil", Number}, {")", Punctuation},
		{"// done", Comment}, {"}", Punctuation}, {"}", Punctuation},
	}},
	{LangSwift, "struct Point { var x: Double = 0 }\nlet p = Point(x: 1.5) // origin\nprint(\"\\(p.x)\")", []tk{
		{"struct", Keyword}, {"Point", Type}, {"{", Punctuation}, {"var", Keyword}, {":", Punctuation}, {"Double", Type},
		{"=", Punctuation}, {"0", Number}, {"}", Punctuation}, {"let", Keyword}, {"=", Punctuation}, {"Point", Type},
		{"(", Punctuation}, {":", Punctuation}, {"1.5", Number}, {")", Punctuation}, {"// origin", Comment}, {"print", Function},
		{"(", Punctuation}, {"\"\\(p.x)\"", String}, {")", Punctuation},
	}},
	{LangJSON, "{\"a\": [1, true, null], \"b\": \"x\\\"y\"}", []tk{
		{"{", Punctuation}, {"\"a\"", Property}, {":", Punctuation}, {"[", Punctuation}, {"1", Number}, {",", Punctuation},
		{"true", Number}, {",", Punctuation}, {"null", Number}, {"],", Punctuation}, {"\"b\"", Property}, {":", Punctuation},
		{"\"x\\\"y\"", String}, {"}", Punctuation},
	}},
	{LangShell, "export PATH=\"$HOME/bin:$PATH\"\nfor f in *.txt; do wc -l \"$f\"; done # count", []tk{
		{"export", Keyword}, {"\"", String}, {"$HOME", Property}, {"/bin:", String}, {"$PATH", Property}, {"\"", String},
		{"for", Keyword}, {"in", Keyword}, {";", Punctuation}, {"do", Keyword}, {"wc", Function}, {"\"", String}, {"$f", Property},
		{"\"", String}, {";", Punctuation}, {"done", Keyword}, {"# count", Comment},
	}},
	{LangSQL, "SELECT name, COUNT(*) FROM users WHERE id = 1 -- one\nGROUP BY name;", []tk{
		{"SELECT", Keyword}, {",", Punctuation}, {"COUNT", Function}, {"(*)", Punctuation}, {"FROM", Keyword}, {"WHERE", Keyword},
		{"=", Punctuation}, {"1", Number}, {"-- one", Comment}, {"GROUP", Keyword}, {"BY", Keyword}, {";", Punctuation},
	}},
	{LangHTML, "<div class=\"a\"><!-- hi --><b>x</b></div>", []tk{
		{"<", Punctuation}, {"div", Keyword}, {"class", Property}, {"=", Punctuation}, {"\"a\"", String}, {">", Punctuation},
		{"<!-- hi -->", Comment}, {"<", Punctuation}, {"b", Keyword}, {">", Punctuation}, {"</", Punctuation}, {"b", Keyword},
		{"></", Punctuation}, {"div", Keyword}, {">", Punctuation},
	}},
	{LangCSS, ".btn:hover { color: #fff; padding: 4px 8px; } /* hover */", []tk{
		{".btn", Type}, {":hover", Keyword}, {"{", Punctuation}, {"color", Property}, {":", Punctuation}, {"#fff", Number},
		{";", Punctuation}, {"padding", Property}, {":", Punctuation}, {"4px", Number}, {"8px", Number}, {";", Punctuation},
		{"}", Punctuation}, {"/* hover */", Comment},
	}},
	{LangYAML, "name: ci\non: [push]\nenv:\n  DEBUG: true # verbose\n  N: 3", []tk{
		{"name", Property}, {":", Punctuation}, {"ci", String}, {"on", Property}, {":", Punctuation}, {"[", Punctuation},
		{"push", String}, {"]", Punctuation}, {"env", Property}, {":", Punctuation}, {"DEBUG", Property}, {":", Punctuation},
		{"true", Number}, {"# verbose", Comment}, {"N", Property}, {":", Punctuation}, {"3", Number},
	}},
	{LangDiff, "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n context\n-old\n+new\n\\ No newline at end of file", []tk{
		{"diff --git a/x b/x", Comment}, {"--- a/x", Comment}, {"+++ b/x", Comment}, {"@@ -1,2 +1,2 @@", Comment}, {"-old", Deleted},
		{"+new", Inserted}, {"\\ No newline at end of file", Comment},
	}},
}

func TestGoldens(t *testing.T) {
	for _, g := range goldens {
		got := toks(g.lang, g.src)
		if fmt.Sprint(got) != fmt.Sprint(g.want) {
			t.Errorf("lang %d:\n got  %v\n want %v", g.lang, got, g.want)
		}
	}
}

func TestTypeScript(t *testing.T) {
	src := "// note\nexport async function load(id: string): Promise<User> {\n  const n = 0x1f + 2.5e-3;\n  return fetch(`/u/${id}`).then(r => r.json()) /* done */;\n}\nconst re = /a[/]b/g, half = n / 2;\nnode.type = null;"
	has(t, LangTS, src, tk{"// note", Comment}, tk{"export", Keyword}, tk{"function", Keyword}, tk{"load", Function}, tk{"string", Type},
		tk{"Promise", Type}, tk{"User", Type}, tk{"0x1f", Number}, tk{"2.5e-3", Number}, tk{"`/u/", String}, tk{"${", Punctuation},
		tk{"fetch", Function}, tk{"json", Function}, tk{"/* done */", Comment}, tk{"/a[/]b/g", String}, tk{"null", Number})
	// A / after a value divides; .type is a property.
	uncolored(t, LangTS, src, "/ 2")
	uncolored(t, LangTS, src, "type")
	uncolored(t, LangTS, "`${id}`", "id")
}

func TestJSX(t *testing.T) {
	src := "return <Card title=\"Hi\" on={() => go(1)}>it's {n} items<br/></Card>;"
	has(t, LangTSX, src, tk{"Card", Type}, tk{"title", Property}, tk{"\"Hi\"", String}, tk{"go", Function}, tk{"1", Number}, tk{"br", Keyword})
	for _, g := range toks(LangTSX, src) {
		if strings.Contains(g.text, "it's") {
			t.Errorf("JSX text colored: %v", g)
		}
	}
	uncolored(t, LangTS, "a <b", "b")
	after := "\nexport function after(n: number) { return n + 1; }"
	for _, head := range []string{"const id = <T,>(x: T) => x;", "type Fn = <T>(x: T) => T;", "const f = <T extends object>(x: T) => x;"} {
		has(t, LangTSX, head+after, tk{"export", Keyword}, tk{"after", Function}, tk{"number", Type}, tk{"1", Number})
	}
	for _, head := range []string{"const el = <div>\n  unclosed text", "const el = <div className=\"a\"", "const el = <div <span>"} {
		has(t, LangTSX, head+"\nexport const after = 1;", tk{"export", Keyword}, tk{"1", Number})
	}
	src = "return <div>{ok && <p>don't</p>}</div>;\nexport const after = 1;"
	has(t, LangTSX, src, tk{"export", Keyword}, tk{"1", Number})
	if n := count(LangTSX, src, "div", Keyword); n != 2 {
		t.Errorf("div tags: %d", n)
	}
}

func TestPython(t *testing.T) {
	src := "def f(a, *, b=None):\n    \"\"\"Doc with 'quotes'\n    and lines.\"\"\"\n    return {k: v for k, v in a.items() if v is not None}\n\nclass Config(BaseModel):\n    PORT = 8080\n    path: list[str] = []\n\n@app.route(\"/x\")\nasync def view(req):\n    await req.send(rb'\\x00', 0o17, 1_000.5j)"
	has(t, LangPython, src, tk{"def", Keyword}, tk{"f", Function}, tk{"None", Number}, tk{"\"\"\"Doc with 'quotes'\n    and lines.\"\"\"", String},
		tk{"items", Function}, tk{"Config", Type}, tk{"BaseModel", Type}, tk{"PORT", Number}, tk{"list", Type}, tk{"@app.route", Keyword},
		tk{"view", Function}, tk{"rb'\\x00'", String}, tk{"0o17", Number}, tk{"1_000.5j", Number})
	uncolored(t, LangPython, src, "v")
	src = "f\"{d['k']:>10} {{literal}} {n + 1}\" + F'{x}' + \"{plain}\""
	has(t, LangPython, src, tk{"f\"", String}, tk{"'k'", String}, tk{" {{literal}} ", String}, tk{"1", Number}, tk{"\"{plain}\"", String})
	if n := count(LangPython, src, "{", Punctuation); n != 3 {
		t.Errorf("f-string fields: %d", n)
	}
	has(t, LangPython, "f'{oops' + 1", tk{"f'{oops'", String}, tk{"1", Number})
	src = "match command.split():\n    case [\"go\", where]:\n        pass\n    case _:  # anything\n        pass\nmatch = re.match(p, s)\ncase(1)"
	if count(LangPython, src, "match", Keyword) != 1 || count(LangPython, src, "case", Keyword) != 2 {
		t.Errorf("soft keywords: %v", toks(LangPython, src))
	}
}

func TestGoRustSwift(t *testing.T) {
	src := "type Server struct {\n\tAddr string\n\tdone chan error\n}\n\nfunc (s *Server) Run(ctx context.Context) error {\n\tpath := `C:\\dir\\`\n\tgo s.loop()\n\treturn NewError(\"x\", iota, 0x1F)\n}"
	has(t, LangGo, src, tk{"Server", Type}, tk{"chan", Keyword}, tk{"error", Type}, tk{"Run", Function}, tk{"`C:\\dir\\`", String},
		tk{"go", Keyword}, tk{"loop", Function}, tk{"NewError", Function}, tk{"iota", Number}, tk{"0x1F", Number})
	uncolored(t, LangGo, src, "Addr")
	uncolored(t, LangGo, src, "Context")
	uncolored(t, LangGo, "s := Server{Addr: \":80\"}", "Addr")

	src = "#[derive(Debug)]\npub fn get<'a>(s: &'a str) -> Option<u8> {\n    let c = 'x';\n    println!(\"{}\", r#\"raw \"q\"\"#);\n    /* outer /* inner */ still */ Some(b'\\n')\n}"
	has(t, LangRust, src, tk{"get", Function}, tk{"'a", Keyword}, tk{"str", Type}, tk{"'x'", String}, tk{"println!", Function},
		tk{"r#\"raw \"q\"\"#", String}, tk{"/* outer /* inner */ still */", Comment}, tk{"b'\\n'", String})

	src = "@MainActor\nstruct Row: View {\n  let title: String = \"\"\"\n  hi\n  \"\"\"\n  func body() -> some View { Text(title).padding(8) }\n}\n#if DEBUG\nlet x = nil"
	has(t, LangSwift, src, tk{"@MainActor", Keyword}, tk{"Row", Type}, tk{"\"\"\"\n  hi\n  \"\"\"", String}, tk{"body", Function},
		tk{"some", Keyword}, tk{"#if", Keyword}, tk{"nil", Number})
}

func TestShellHTMLCSSSQLYAML(t *testing.T) {
	src := "#!/bin/bash\nFOO=1 npm run build --if-present # build\nif [ -n \"$HOME/${X}\" ]; then\n  echo $(git rev-parse HEAD) | tee out.txt\nfi\ncat <<EOF\n$not a var\nEOF\nls"
	has(t, LangShell, src, tk{"#!/bin/bash", Comment}, tk{"FOO", Property}, tk{"npm", Function}, tk{"# build", Comment}, tk{"if", Keyword},
		tk{"[", Punctuation}, tk{"$HOME", Property}, tk{"${X}", Property}, tk{"$(", Punctuation}, tk{"git", Function}, tk{"tee", Function},
		tk{"EOF", String}, tk{"ls", Function})
	uncolored(t, LangShell, src, "run")
	if count(LangShell, "cargo build && \\\n  cargo test", "cargo", Function) != 2 {
		t.Error("line continuation")
	}
	if count(LangShell, "n=$(echo $#); echo after", "echo", Function) != 2 {
		t.Error("$# taken as a comment")
	}

	src = "<!-- c -->\n<a href=\"/x\" data-id=7>Tom &amp; Jerry</a>\n<script>const a = 1;</script>\n<style>p { color: red; }</style>"
	has(t, LangHTML, src, tk{"<!-- c -->", Comment}, tk{"href", Property}, tk{"7", String}, tk{"&amp;", Number}, tk{"const", Keyword},
		tk{"1", Number}, tk{"color", Property})
	uncolored(t, LangHTML, src, "Tom")

	src = "@media (min-width: 600px) {\n  .card:hover > a, #main { color: #fff !important; margin: -4px 0 calc(1em + 2%); --gap: 4px; }\n}"
	has(t, LangCSS, src, tk{"@media", Keyword}, tk{"600px", Number}, tk{".card", Type}, tk{"#main", Type}, tk{"!important", Keyword},
		tk{"-4px", Number}, tk{"calc", Function}, tk{"2%", Number}, tk{"--gap", Property})

	src = "-- users\nSELECT id, count(*) AS n FROM \"users\" WHERE name = 'O''Brien' AND age > $1 LIMIT 10;\nCREATE TABLE t (id uuid PRIMARY KEY, ok boolean DEFAULT false);"
	has(t, LangSQL, src, tk{"count", Function}, tk{"\"users\"", Property}, tk{"'O''Brien'", String}, tk{"$1", Property}, tk{"uuid", Type},
		tk{"false", Number})

	src = "---\nname: rune # the name\nversion: 1.2\nanchors: &base\n  - item\n  - key: \"v\"\n    on: true\nscript: |\n  echo hi\n  # not a comment\nflow: [a, 1, {b: c}]\nafter: x"
	has(t, LangYAML, src, tk{"---", Punctuation}, tk{"rune", String}, tk{"# the name", Comment}, tk{"1.2", Number}, tk{"&base", Type},
		tk{"echo hi", String}, tk{"# not a comment", String}, tk{"b", Property}, tk{"c", String}, tk{"after", Property})
}

var allLangs = []Lang{LangJS, LangTS, LangTSX, LangRust, LangSwift, LangGo, LangPython, LangShell, LangHTML, LangCSS, LangSQL, LangYAML, LangJSON, LangDiff}

func TestRangesAreOrderedAndOnCharBoundaries(t *testing.T) {
	delim := strings.Repeat("a", 63) + "é"
	heredoc := "cat << " + delim + "\nhi\n" + delim + "\nls"
	has(t, LangShell, heredoc, tk{"cat", Function}, tk{"hi\n" + delim, String}, tk{"ls", Function})
	pieces := []string{
		"\"", "'", "`", "\"\"\"", "'''", "r#\"", "\"#", "b\"", "b'", "f\"", "f'{", "${", "$(", "/*", "*/", "//",
		"<!--", "-->", "<![CDATA[", "]]>", "<script>", "</script>", "<style>", "<<EOF\n", "<<-\t", "\\", "\\uD800",
		"\\u{D800}", "\\x00", "\r\n", "\r", "\n", "\t", " ", "é", "🎉", "日本語", "a", "1_000.5e-2", ":", "|", ">",
		"- ", "@", "#", "$", "&amp;", "=", "(", ")", "[", "]", "{", "}", "/", "+",
	}
	srcs := []string{
		"const café = \"naïve 🎉\"; // ✓\n<p>é</p> `${'ü'}`",
		"echo \\🎉 123 \\é",
		heredoc,
		"let x = 1;\n" + strings.Repeat("🎉", MaxSource/4+4),
	}
	seed := uint64(0x9e3779b97f4a7c15)
	for n := 1; n <= 64; n++ {
		var b strings.Builder
		for range n {
			seed = seed*6364136223846793005 + 1
			b.WriteString(pieces[(seed>>32)%uint64(len(pieces))])
		}
		srcs = append(srcs, b.String())
	}
	ok := func(s string, i int) bool { return i == len(s) || utf8.RuneStart(s[i]) }
	for _, src := range srcs {
		end := ColoredLen(src)
		if !ok(src, end) {
			t.Fatalf("cap splits a character: %q", src)
		}
		for _, lang := range allLangs {
			var raw out
			scan(lang, src, 0, end, &raw, 0)
			for _, tk := range raw.tokens {
				if !ok(src, tk.Start) || !ok(src, tk.End) {
					t.Errorf("lang %d: raw range %v splits a character in %q", lang, tk, src)
				}
			}
			prev := 0
			for _, tk := range Scan(lang, src) {
				if tk.Start < prev || tk.Start >= tk.End || tk.End > end {
					t.Errorf("lang %d: bad range %v after %d in %q", lang, tk, prev, src)
				}
				prev = tk.End
			}
		}
	}
}

func TestHostileInputStaysLinearAndBounded(t *testing.T) {
	n := MaxSource - 64
	inputs := []string{
		strings.Repeat("\"", n), strings.Repeat("/*", n/2), strings.Repeat("`${", n/3), strings.Repeat("{", n),
		strings.Repeat("<a b={", n/6), strings.Repeat("(/", n/2), "r" + strings.Repeat("#", n) + "\"", strings.Repeat("$($(", n/4),
		strings.Repeat("'", n), strings.Repeat("a", n), strings.Repeat("<script>", n/8), strings.Repeat("- ", n/2),
		strings.Repeat("x: |\n", n/5), strings.Repeat("@media{", n/7), strings.Repeat("f'{", n/3), strings.Repeat("f\"{'", n/4),
		strings.Repeat("\"\"\"", n/3), strings.Repeat("`", n), strings.Repeat("match x\n", n/8),
	}
	t0 := time.Now()
	for _, src := range inputs {
		for _, lang := range allLangs {
			if got := Scan(lang, src); len(got) > MaxTokens {
				t.Errorf("lang %d: %d tokens", lang, len(got))
			}
		}
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("hostile inputs took %v", d)
	}
}

func TestColourStopsAtTheCaps(t *testing.T) {
	line := "// one comment per line, each its own range\n"
	big := strings.Repeat(line, MaxSource/len(line)+10)
	got := Scan(LangRust, big)
	if last := got[len(got)-1].End; last > MaxSource || last <= MaxSource-len(line) {
		t.Errorf("last range ends at %d", last)
	}
	if got := Scan(LangJS, strings.Repeat("1 ", MaxTokens+10)); len(got) != MaxTokens {
		t.Errorf("%d tokens, want %d", len(got), MaxTokens)
	}
}
