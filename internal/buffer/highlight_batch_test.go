package buffer

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/pkg/highlight"
)

// Batching also touches the default regex path. Its final region states and
// line matches must agree with a fresh full highlight after multi-line edits.
func TestRegexBatchedHighlighting(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = false
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("package main\n/* old\ncomment */\nfunc f() { println(`raw\ntext`) }\n", "regex-batch.go", BTDefault)
	defer b.Close()
	b.syntaxMu.Lock()
	b.Highlighter.HighlightStates(b)
	b.Highlighter.HighlightMatches(b, 0, b.LinesNum()-1)
	b.syntaxMu.Unlock()
	check := func() {
		t.Helper()
		b.syntaxMu.Lock()
		defer b.syntaxMu.Unlock()
		text := b.Bytes()
		fresh := NewLineArray(uint64(len(text)), b.Endings, bytes.NewReader(text))
		h := highlight.NewHighlighter(b.SyntaxDef)
		h.HighlightStates(fresh)
		h.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
		for i := 0; i < b.LinesNum(); i++ {
			if !reflect.DeepEqual(b.Match(i), fresh.Match(i)) {
				t.Fatalf("regex batch left stale row %d", i)
			}
		}
	}
	b.MultipleReplace([]Delta{{Start: Loc{0, 1}, End: Loc{2, 1}, Text: []byte("//")}, {Start: Loc{8, 2}, End: Loc{10, 2}, Text: []byte("  ")}})
	check()
	b.UndoOneEvent()
	check()
	b.RedoOneEvent()
	check()
	b.ApplyDiff("package main\nfunc café() {\n\tprintln(\"é\")\n}\n/* tail */\n")
	check()
	b.Retab()
	check()
}

func TestMissingGrammarUsesIncrementalRegex(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("# first\nx = 1\n# last\n", "regex-fallback.py", BTDefault)
	defer b.Close()
	b.syntaxMu.Lock()
	// Complete initialization synchronously and supersede any queued refresh.
	b.syntaxGeneration++
	b.Highlighter.HighlightStates(b)
	b.Highlighter.HighlightMatches(b, 0, b.LinesNum()-1)
	if b.Highlighter.TreeSitter {
		b.syntaxMu.Unlock()
		t.Fatal("missing grammar selected the Tree-sitter update path")
	}
	b.syntaxMu.Unlock()
	before := reflect.ValueOf(b.Match(2)).Pointer()
	b.Insert(Loc{0, 0}, " ")
	b.EnsureHighlights(0, 0)
	if before != reflect.ValueOf(b.Match(2)).Pointer() {
		t.Fatal("local edit rehighlighted an unchanged offscreen regex row")
	}
}
