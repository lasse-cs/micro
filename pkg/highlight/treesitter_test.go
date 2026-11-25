//go:build treesitter && cgo

package highlight

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/micro-editor/micro/v2/internal/tstest"
)

type testLines struct {
	sync.Mutex
	lines   [][]byte
	states  []State
	matches []LineMatch
}

func newTestLines(text string) *testLines {
	lines := strings.Split(text, "\n")
	b := &testLines{states: make([]State, len(lines)), matches: make([]LineMatch, len(lines))}
	for _, line := range lines {
		b.lines = append(b.lines, []byte(line))
	}
	return b
}
func (b *testLines) LineBytes(n int) []byte      { return b.lines[n] }
func (b *testLines) LinesNum() int               { return len(b.lines) }
func (b *testLines) State(n int) State           { return b.states[n] }
func (b *testLines) SetState(n int, s State)     { b.states[n] = s }
func (b *testLines) SetMatch(n int, m LineMatch) { b.matches[n] = m }
func goTestHighlighter(t testing.TB) *Highlighter {
	t.Helper()
	if !TreeSitterAvailable("go") {
		parser, query := tstest.GoFixture(t)
		err := RegisterTreeSitterLanguage(TreeSitterLanguageSpec{
			Name: "go", Filetypes: []string{"go"}, Parser: parser, Highlights: query,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return nativeTestHighlighter(t)
}

func nativeTestHighlighter(t testing.TB) *Highlighter {
	t.Helper()
	data, err := os.ReadFile("../../runtime/syntax/go.yaml")
	if err != nil {
		t.Fatal(err)
	}
	header, err := MakeHeaderYaml(data)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseFile(data)
	if err != nil {
		t.Fatal(err)
	}
	def, err := ParseDef(file, header)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHighlighter(def)
	h.TreeSitter = true
	t.Cleanup(h.ResetTreeSitter)
	return h
}
func groupAt(b *testLines, row, column int) Group {
	var group Group
	for i := 0; i <= column; i++ {
		if g, ok := b.matches[row][i]; ok {
			group = g
		}
	}
	return group
}
func assertToken(t *testing.T, b *testLines, row int, token, group string) {
	t.Helper()
	offset := strings.Index(string(b.lines[row]), token)
	if offset < 0 {
		t.Fatalf("missing token %q", token)
	}
	column := CharacterCount(b.lines[row][:offset])
	if got := groupAt(b, row, column); got.Fallback() != Groups[group] {
		t.Fatalf("%q: got %q, want %q", token, got.String(), group)
	}
}
func TestTreeSitterGo(t *testing.T) {
	h := goTestHighlighter(t)
	b := newTestLines("package main\nfunc café() {\n\t变量 := `héllo\nworld`\n\tprintln(变量) // comment\n\t_ = \"é🙂\"; café()\n\t_ = \"a\\nb\"\n}\n")
	h.HighlightStates(b)
	h.HighlightMatches(b, 0, b.LinesNum()-1)
	if !h.treeSitterActive {
		t.Fatal("Tree-sitter did not activate")
	}
	for _, test := range []struct {
		row          int
		token, group string
	}{{0, "package", "preproc"}, {1, "café", "identifier"}, {2, "héllo", "constant.string"}, {3, "world", "constant.string"}, {4, "println", "special"}, {4, "//", "comment"}, {5, "café", "identifier"}, {6, "\\n", "constant.specialChar"}} {
		assertToken(t, b, test.row, test.token, test.group)
	}
	// Remove the raw-string opener; a full reparse must update later lines too.
	h.EditTreeSitter(2, 0, 2, len(b.lines[2]), []byte("\t变量 := 42"))
	b.lines[2] = []byte("\t变量 := 42")
	h.ReHighlightStates(b, 2)
	h.HighlightMatches(b, 2, 2)
	fresh := newTestLines(strings.Join(func() []string {
		var s []string
		for _, l := range b.lines {
			s = append(s, string(l))
		}
		return s
	}(), "\n"))
	other := goTestHighlighter(t)
	other.HighlightStates(fresh)
	other.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
	h.HighlightMatches(b, 0, b.LinesNum()-1) // Simulate visiting all rows before comparison.
	if !reflect.DeepEqual(b.matches, fresh.matches) {
		t.Fatal("edit left stale highlights")
	}
}
func TestTreeSitterFallback(t *testing.T) {
	for _, reason := range []string{"disabled", "size", "unsupported"} {
		t.Run(reason, func(t *testing.T) {
			h := goTestHighlighter(t)
			b := newTestLines("package main\nfunc f() {}\n// tail")
			h.HighlightStates(b)
			h.HighlightMatches(b, 0, 2)
			if !h.treeSitterActive {
				t.Fatal("backend did not activate")
			}
			switch reason {
			case "disabled":
				h.TreeSitter = false
			case "size":
				h.EditTreeSitter(2, 0, 2, len(b.lines[2]), []byte("//"+strings.Repeat("x", treeSitterMaxBytes)))
				b.lines[2] = []byte("//" + strings.Repeat("x", treeSitterMaxBytes))
			case "unsupported":
				h.Def.FileType = "unsupported"
			}
			h.HighlightMatches(b, 2, 2)
			if h.treeSitterActive {
				t.Fatal("backend did not fall back")
			}
			want := newTestLines(string(b.lines[0]) + "\n" + string(b.lines[1]) + "\n" + string(b.lines[2]))
			regex := NewHighlighter(h.Def)
			regex.HighlightStates(want)
			regex.HighlightMatches(want, 0, 2)
			if !reflect.DeepEqual(b.matches, want.matches) {
				t.Fatal("fallback differs from regex / stale Tree-sitter results")
			}
		})
	}
}
func BenchmarkTreeSitterGo(b *testing.B) {
	h := goTestHighlighter(b)
	lines := newTestLines("package main\n" + strings.Repeat("func f() { println(123) }\n", 400))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ResetTreeSitter()
		if !h.highlightTreeSitter(lines, 0, lines.LinesNum()-1) {
			b.Fatal("fell back")
		}
	}
}

func TestTreeSitterIncrementalReuse(t *testing.T) {
	h := goTestHighlighter(t)
	b := newTestLines("package main\n" + strings.Repeat("func f() { foo(123) }\n", 80))
	h.HighlightStates(b)
	h.HighlightMatches(b, 0, b.LinesNum()-1)
	s := h.treeSitter
	if s == nil {
		t.Fatal("initial parse fell back")
	}
	initialLines := s.highlightedLines
	untouched := b.matches[5]
	// Two edits are fed before one parse (equivalent to a replacement event).
	h.EditTreeSitter(40, 11, 40, 14, nil)
	h.EditTreeSitter(40, 11, 40, 11, []byte("len"))
	b.lines[40] = []byte("func f() { len(123) }")
	h.HighlightMatches(b, 40, 40)
	if h.treeSitter != s || s.parses != 2 || s.incrementalParses != 1 || s.parsedRevision != 2 {
		t.Fatalf("state not reused/batched: %+v", s)
	}
	if changed := s.highlightedLines - initialLines; changed > 3 {
		t.Fatalf("tiny edit repainted %d lines", changed)
	}
	assertToken(t, b, 40, "len", "preproc")
	// Map identity checks catch accidental whole-document replacement.
	if reflect.ValueOf(untouched).Pointer() != reflect.ValueOf(b.matches[5]).Pointer() {
		t.Fatal("untouched line map replaced")
	}
	fresh := newTestLines(strings.Join(func() []string {
		var lines []string
		for _, line := range b.lines {
			lines = append(lines, string(line))
		}
		return lines
	}(), "\n"))
	full := goTestHighlighter(t)
	full.HighlightStates(fresh)
	full.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
	h.HighlightMatches(b, 0, b.LinesNum()-1) // Simulate visiting all rows before comparison.
	if !reflect.DeepEqual(b.matches, fresh.matches) {
		t.Fatal("incremental result differs from full query")
	}
	// Theme/rules refresh recomputes colours without parsing the document again.
	h.RefreshTreeSitter()
	h.HighlightMatches(b, 0, b.LinesNum()-1)
	if s.parses != 2 {
		t.Fatal("colour refresh reparsed text")
	}
	h.ResetTreeSitter()
	if h.treeSitter != nil {
		t.Fatal("reset retained native state")
	}
}

func BenchmarkTreeSitterIncremental(b *testing.B) {
	h := goTestHighlighter(b)
	lines := newTestLines("package main\n" + strings.Repeat("func f() { foo(123) }\n", 400))
	h.HighlightMatches(lines, 0, lines.LinesNum()-1)
	if h.treeSitter == nil {
		b.Fatal("initial parse fell back")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		word := "len"
		if i%2 != 0 {
			word = "foo"
		}
		h.EditTreeSitter(200, 11, 200, 14, []byte(word))
		copy(lines.lines[200][11:14], word)
		h.HighlightMatches(lines, 200, 200)
		if h.treeSitter == nil {
			b.Fatal("incremental update fell back")
		}
	}
}

func TestTreeSitterFallbackRecovery(t *testing.T) {
	h := goTestHighlighter(t)
	b := newTestLines("package main\nfunc f() {}\n// end")
	h.HighlightStates(b)
	h.HighlightMatches(b, 0, 2)
	h.EditTreeSitter(2, 0, 2, len(b.lines[2]), []byte(strings.Repeat("x", treeSitterMaxBytes)))
	b.lines[2] = []byte(strings.Repeat("x", treeSitterMaxBytes))
	h.HighlightStates(b)
	h.HighlightMatches(b, 2, 2)
	if h.treeSitter != nil || h.TreeSitterActive() {
		t.Fatal("oversize fallback retained parser/tree")
	}
	// Edits while on fallback must recover by parsing the current complete input.
	h.EditTreeSitter(2, 0, 2, len(b.lines[2]), []byte("// back"))
	b.lines[2] = []byte("// back")
	h.HighlightStates(b)
	h.HighlightMatches(b, 2, 2)
	if !h.TreeSitterActive() {
		t.Fatal("did not recover after shrinking document")
	}
	h.HighlightMatches(b, 1, 1) // Scroll back to a row previously painted by regex.
	assertToken(t, b, 1, "f()", "identifier")
	assertToken(t, b, 2, "//", "comment")
}

// Same source and edits as BenchmarkTreeSitterIncremental, but deliberately
// discard cached state to measure the full-parse/full-highlight control.
func BenchmarkTreeSitterFullEdit(b *testing.B) {
	h := goTestHighlighter(b)
	lines := newTestLines("package main\n" + strings.Repeat("func f() { foo(123) }\n", 400))
	fallbacks := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		word := "len"
		if i%2 != 0 {
			word = "foo"
		}
		copy(lines.lines[200][11:14], word)
		h.ResetTreeSitter()
		h.HighlightMatches(lines, 0, lines.LinesNum()-1)
		if h.treeSitter == nil {
			fallbacks++
		}
	}
	b.ReportMetric(float64(fallbacks)/float64(b.N), "fallbacks/op")
}

// A viewport can start inside a capture, and an offscreen edit can change its
// classification without touching any visible row. Exercise both viewports and
// compare each visited range with an independent full query.
func TestTreeSitterViewportCache(t *testing.T) {
	h := goTestHighlighter(t)
	b := newTestLines("package main\n/*\n" + strings.Repeat("func f() { foo(123) }\n", 100) + "*/\n")
	h.HighlightMatches(b, 40, 49)
	s := h.treeSitter
	if s == nil || s.highlightedLines != 10 || b.matches[1] != nil {
		t.Fatal("cold viewport must not paint offscreen rows")
	}
	assertToken(t, b, 40, "func", "comment")
	h.HighlightMatches(b, 40, 49)
	if s.parses != 1 || s.highlightedLines != 10 {
		t.Fatal("unchanged redraw did work")
	}
	h.HighlightMatches(b, 45, 54)
	if s.highlightedLines != 15 {
		t.Fatal("scroll did not reuse overlapping rows")
	}
	h.HighlightMatches(b, 80, 84) // second split
	if s.parses != 1 || s.highlightedLines != 20 {
		t.Fatal("split did not share parse/cache")
	}
	// Removing an opener above both windows invalidates both, lazily.
	h.EditTreeSitter(1, 0, 1, 2, []byte("//"))
	b.lines[1] = []byte("//")
	if s.parses != 1 || s.highlightedLines != 20 {
		t.Fatal("edit eagerly parsed or painted")
	}
	h.HighlightMatches(b, 40, 49)
	if s.incrementalParses != 1 || s.highlightedLines != 30 {
		t.Fatal("edit redraw must reuse tree and paint requested range")
	}
	assertToken(t, b, 40, "f()", "identifier")
	h.HighlightMatches(b, 80, 84)
	if s.parses != 2 || s.highlightedLines != 35 {
		t.Fatal("second split reparsed or retained stale cache")
	}
	var lines []string
	for _, line := range b.lines {
		lines = append(lines, string(line))
	}
	fresh := newTestLines(strings.Join(lines, "\n"))
	other := goTestHighlighter(t)
	other.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
	for _, r := range [][2]int{{40, 49}, {80, 84}} {
		for row := r[0]; row <= r[1]; row++ {
			if !reflect.DeepEqual(b.matches[row], fresh.matches[row]) {
				t.Fatalf("stale viewport row %d", row)
			}
		}
	}
}

func BenchmarkTreeSitterViewportEdit(b *testing.B) {
	h := goTestHighlighter(b)
	lines := newTestLines("package main\n" + strings.Repeat("func f() { foo(123) }\n", 400))
	h.HighlightMatches(lines, 180, 219)
	fallbacks := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		word := "len"
		if i%2 != 0 {
			word = "foo"
		}
		h.EditTreeSitter(200, 11, 200, 14, []byte(word))
		copy(lines.lines[200][11:14], word)
		h.HighlightMatches(lines, 180, 219)
		if !h.TreeSitterActive() {
			fallbacks++
		}
	}
	b.ReportMetric(float64(fallbacks)/float64(b.N), "fallbacks/op")
}
