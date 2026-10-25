//go:build treesitter && cgo && linux

package highlight

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func externalSpec(t *testing.T, name string) TreeSitterLanguageSpec {
	t.Helper()
	dir := os.Getenv("MICRO_TS_PARSER_DIR")
	if dir == "" {
		t.Skip("set MICRO_TS_PARSER_DIR to a directory containing Python, JavaScript, HTML and CSS parser libraries")
	}
	// Minimal fixtures keep core loader tests independent of language plugins.
	source := map[string]string{
		"python":     "(identifier) @function\n(comment) @comment",
		"javascript": "(identifier) @function\n(comment) @comment",
		"html":       "(tag_name) @tag\n(comment) @comment",
		"css":        "(property_name) @type\n(comment) @comment",
	}[name]
	if source == "" {
		t.Fatalf("no query fixture for %s", name)
	}
	query := filepath.Join(t.TempDir(), "highlights.scm")
	if err := os.WriteFile(query, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return TreeSitterLanguageSpec{Name: name, Parser: filepath.Join(dir, name+".so"), Highlights: query, Filetypes: []string{"external-" + name}}
}
func TestExternalLanguages(t *testing.T) {
	for _, name := range []string{"python", "javascript"} {
		t.Run(name, func(t *testing.T) {
			spec := externalSpec(t, name)
			if err := RegisterTreeSitterLanguage(spec); err != nil {
				t.Fatal(err)
			}
			code := "def greet(name):\n    return name\ngreet('Ada')"
			row := 2
			if name == "javascript" {
				code = "function greet(name) { return name; }\ngreet('Ada');"
				row = 1
			}
			h := testHighlighter(t)
			h.Def.FileType = spec.Filetypes[0]
			b := newTestLines(code)
			h.HighlightStates(b)
			h.HighlightMatches(b, 0, b.LinesNum()-1)
			if !h.TreeSitterActive() {
				t.Fatal("external backend fell back")
			}
			assertToken(t, b, row, "greet", "identifier")
			old := h.treeSitter
			linesBefore := old.highlightedLines
			h.EditTreeSitter(row, 0, row, 5, []byte("hello"))
			copy(b.lines[row][:5], "hello")
			h.HighlightMatches(b, row, row)
			if h.treeSitter != old || old.incrementalParses != 1 {
				t.Fatal("external parser was not reused incrementally")
			}
			if old.highlightedLines-linesBefore != 1 {
				t.Fatal("external language must paint only requested rows")
			}
			var text []string
			for _, line := range b.lines {
				text = append(text, string(line))
			}
			fresh := newTestLines(strings.Join(text, "\n"))
			other := testHighlighter(t)
			other.Def.FileType = spec.Filetypes[0]
			other.HighlightStates(fresh)
			other.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
			h.HighlightMatches(b, 0, b.LinesNum()-1)
			if !other.TreeSitterActive() || !reflect.DeepEqual(b.matches, fresh.matches) {
				t.Fatal("incremental external output differs from fresh parse")
			}
			// Reload only a capture mapping. Existing documents pick up the immutable
			// replacement registration and discard incompatible cached parser state.
			spec.Captures = map[string]string{"function": "type"}
			if err := RegisterTreeSitterLanguage(spec); err != nil {
				t.Fatal(err)
			}
			h.HighlightMatches(b, 0, b.LinesNum()-1)
			if !h.TreeSitterActive() || h.treeSitter == old {
				t.Fatal("registration reload did not replace document state")
			}
			assertToken(t, b, row, "hello", "type")
		})
	}
}

func TestExternalRegistrationErrorsAreAtomic(t *testing.T) {
	good := externalSpec(t, "python")
	if err := RegisterTreeSitterLanguage(good); err != nil {
		t.Fatal(err)
	}
	original := lookupLanguage(good.Filetypes[0])
	query := filepath.Join(t.TempDir(), "highlights.scm")
	cases := []struct {
		name, source string
		change       func(*TreeSitterLanguageSpec)
	}{
		{name: "missing library", change: func(s *TreeSitterLanguageSpec) { s.Parser = "/definitely/missing/parser.so" }},
		{name: "invalid shared library", change: func(s *TreeSitterLanguageSpec) { s.Parser = s.Highlights }},
		{name: "relative path", change: func(s *TreeSitterLanguageSpec) { s.Parser = "python.so" }},
		{name: "missing symbol", change: func(s *TreeSitterLanguageSpec) { s.Symbol = "tree_sitter_missing" }},
		{name: "malformed query", source: "(this_node_does_not_exist) @function"},
		{name: "unknown capture", source: "(identifier) @not_mapped"},
		{name: "unsupported directive", source: "((identifier) @function (#set! priority 100))"},
		{name: "unsupported predicate", source: "((identifier) @function (#lua-match? @function \"x\"))"},
		{name: "inheritance", source: "; inherits: python\n(identifier) @function"},
		{name: "bad group", change: func(s *TreeSitterLanguageSpec) { s.Captures = map[string]string{"function": "unknown-colour"} }},
		{name: "filetype conflict", change: func(s *TreeSitterLanguageSpec) { s.Name = "other_python"; s.Symbol = "tree_sitter_python" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			spec := good
			if test.source != "" {
				if err := os.WriteFile(query, []byte(test.source), 0600); err != nil {
					t.Fatal(err)
				}
				spec.Highlights = query
			}
			if test.change != nil {
				test.change(&spec)
			}
			if err := RegisterTreeSitterLanguage(spec); err == nil {
				t.Fatal("invalid registration accepted")
			}
			if lookupLanguage(good.Filetypes[0]) != original {
				t.Fatal("failure replaced working registration")
			}
		})
	}
	// Helper captures participate in predicates but must never paint default over
	// another capture at the same range.
	if err := os.WriteFile(query, []byte("((identifier) @function @_name (#eq? @_name \"hello\"))"), 0600); err != nil {
		t.Fatal(err)
	}
	good.Highlights = query
	if err := RegisterTreeSitterLanguage(good); err != nil {
		t.Fatal(err)
	}
	h := testHighlighter(t)
	h.Def.FileType = good.Filetypes[0]
	b := newTestLines("hello()")
	h.HighlightMatches(b, 0, 0)
	if !h.TreeSitterActive() {
		t.Fatal("helper query fell back")
	}
	assertToken(t, b, 0, "hello", "identifier")
}

func TestExternalBadABI(t *testing.T) {
	spec := externalSpec(t, "python")
	dir := t.TempDir()
	// The version is the first field of the Tree-sitter language ABI. The runtime
	// must reject this descriptor before reading any further fields.
	source := filepath.Join(dir, "bad.c")
	if err := os.WriteFile(source, []byte("#include <stdint.h>\nstatic const uint32_t language[] = {0};\nconst void *tree_sitter_python(void) {return language;}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(dir, "bad.so")
	compiler := os.Getenv("CC")
	if compiler == "" {
		compiler = "cc"
	}
	if output, err := exec.Command(compiler, "-shared", "-fPIC", source, "-o", library).CombinedOutput(); err != nil {
		t.Fatalf("compile ABI fixture: %v: %s", err, output)
	}
	spec.Parser = library
	if err := RegisterTreeSitterLanguage(spec); err == nil || !strings.Contains(err.Error(), "ABI") {
		t.Fatalf("bad ABI was not rejected: %v", err)
	}
}

func TestExternalHTMLCSSViewport(t *testing.T) {
	for _, tc := range []struct {
		name, code, oldToken, newToken, group string
		row, column                           int
	}{
		{"html", "<!-- above\ncontinued -->\n<main>\n<custom-card class=\"card\">&amp;</custom-card>\n</main>", "custom-card", "other-panel", "symbol.tag", 3, 1},
		{"css", "/* above\ncontinued */\n.card {\n  color: #abc;\n  padding: 1.5rem;\n}", "color", "width", "type", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := externalSpec(t, tc.name)
			if err := RegisterTreeSitterLanguage(spec); err != nil {
				t.Fatal(err)
			}
			h := testHighlighter(t)
			h.Def.FileType = spec.Filetypes[0]
			b := newTestLines(tc.code)
			h.HighlightMatches(b, tc.row, tc.row)
			if !h.TreeSitterActive() {
				t.Fatal("native backend unavailable")
			}
			assertToken(t, b, tc.row, tc.oldToken, tc.group)
			state := h.treeSitter
			if state.highlightedLines != 1 || b.matches[0] != nil {
				t.Fatal("painted outside viewport")
			}
			h.EditTreeSitter(tc.row, tc.column, tc.row, tc.column+len(tc.oldToken), []byte(tc.newToken))
			b.lines[tc.row] = []byte(strings.Replace(string(b.lines[tc.row]), tc.oldToken, tc.newToken, 1))
			h.HighlightMatches(b, tc.row, tc.row)
			if h.treeSitter != state || state.incrementalParses != 1 || state.highlightedLines != 2 {
				t.Fatal("edit did not reuse parser/ranged query")
			}
			assertToken(t, b, tc.row, tc.newToken, tc.group)
			// Scroll upwards into a multiline comment that began offscreen.
			h.HighlightMatches(b, 1, 1)
			assertToken(t, b, 1, "continued", "comment")
			var lines []string
			for _, line := range b.lines {
				lines = append(lines, string(line))
			}
			fresh := newTestLines(strings.Join(lines, "\n"))
			other := testHighlighter(t)
			other.Def.FileType = spec.Filetypes[0]
			other.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
			h.HighlightMatches(b, 0, b.LinesNum()-1)
			if !other.TreeSitterActive() || !reflect.DeepEqual(b.matches, fresh.matches) {
				t.Fatal("viewport highlights differ from full query")
			}
		})
	}
}
