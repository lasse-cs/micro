//go:build treesitter && cgo

package buffer

import (
	"bytes"
	"github.com/micro-editor/micro/v2/internal/util"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/pkg/highlight"
)

func TestTreeSitterEditing(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("package main\nfunc café() { println(\"é🙂\") }\n", "treesitter-test.go", BTDefault)
	defer b.Close()
	if b.SyntaxDef == nil || b.FileType() != "go" || !b.Highlighter.TreeSitter {
		t.Fatal("Go Tree-sitter configuration not selected")
	}
	b.EnsureHighlights(0, b.LinesNum()-1)
	// Function names are not captured by the old Go regex definition.
	if b.Match(1)[5].Fallback() != highlight.Groups["identifier"] {
		t.Fatal("initial Tree-sitter function colour missing")
	}
	assertFresh := func() { t.Helper(); assertTreeSitterFresh(t, b) }

	b.Insert(Loc{0, 1}, "/* multiline\ncomment */\n")
	assertFresh()
	b.UndoOneEvent()
	assertFresh()
	b.RedoOneEvent()
	assertFresh()
	b.Remove(Loc{0, 1}, Loc{0, 3})
	assertFresh()
	b.UndoOneEvent()
	assertFresh()
	b.UpdateRules()
	assertFresh() // same path used by theme changes
	if err := b.SetOption("syntax", "false"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < b.LinesNum(); i++ {
		if len(b.Match(i)) != 0 {
			t.Fatal("syntax off retained colours")
		}
	}
	if err := b.SetOption("syntax", "true"); err != nil {
		t.Fatal(err)
	}
	assertFresh()
}

// Compare independent fresh output without replacing the live cached line maps.
func assertTreeSitterFresh(t *testing.T, b *Buffer) {
	t.Helper()
	b.EnsureHighlights(0, b.LinesNum()-1)
	b.syntaxMu.Lock()
	defer b.syntaxMu.Unlock()
	text := b.Bytes()
	fresh := NewLineArray(uint64(len(text)), b.Endings, bytes.NewReader(text))
	h := highlight.NewHighlighter(b.SyntaxDef)
	// A budget-expired live update is allowed to use regex. Compare it with
	// fresh regex output, rather than incorrectly demanding Tree-sitter colours.
	h.TreeSitter = b.Highlighter.TreeSitterActive()
	defer h.ResetTreeSitter()
	for attempt := 0; attempt < 3; attempt++ {
		h.HighlightStates(fresh)
		h.HighlightMatches(fresh, 0, fresh.LinesNum()-1)
		if !h.TreeSitter || h.TreeSitterActive() {
			break
		}
	}
	if h.TreeSitter && !h.TreeSitterActive() {
		t.Fatal("fresh reference repeatedly exceeded Tree-sitter budget")
	}
	for i := 0; i < b.LinesNum(); i++ {
		if !reflect.DeepEqual(b.Match(i), fresh.Match(i)) {
			t.Fatalf("stale highlights at row %d, text=%q: got %v, want %v", i, b.Line(i), b.Match(i), fresh.Match(i))
		}
	}
}

func TestTreeSitterEditSequences(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("package main\nfunc café() {\n\tfoo(123)\n\t_ = `raw\ntext`\n}\nfunc g() {}\n", "incremental-sequences.go", BTDefault)
	defer b.Close()
	// Identical syntax shape, different text predicate: foo -> len.
	b.Remove(Loc{1, 2}, Loc{4, 2})
	b.Insert(Loc{1, 2}, "len")
	assertTreeSitterFresh(t, b)
	if b.Match(2)[1].Fallback() != highlight.Groups["preproc"] {
		t.Fatal("builtin predicate not refreshed")
	}
	// CRLF paste, multiline removal, undo/redo, and whitespace rewriting.
	b.Insert(Loc{0, 1}, "// pasted\r\n// café 🙂\r\n")
	assertTreeSitterFresh(t, b)
	b.UndoOneEvent()
	assertTreeSitterFresh(t, b)
	b.RedoOneEvent()
	assertTreeSitterFresh(t, b)
	b.Retab()
	assertTreeSitterFresh(t, b)
	// Repeated replacements at random valid character boundaries exercise error
	// recovery and shifting multiline captures, not just syntactically valid Go.
	rng := rand.New(rand.NewSource(73))
	inserts := []string{"x", "len", "\n", "/*", "*/", "`", "\"", "é", "é", "🙂", "\r\n", "{}", " "}
	for step := 0; step < 160; step++ {
		row := rng.Intn(b.LinesNum())
		n := util.CharacterCount(b.LineBytes(row))
		col := rng.Intn(n + 1)
		if n > col && rng.Intn(3) == 0 {
			b.Remove(Loc{col, row}, Loc{col + 1, row})
		} else {
			b.Insert(Loc{col, row}, inserts[rng.Intn(len(inserts))])
		}
		assertTreeSitterFresh(t, b)
		if step%11 == 0 {
			b.UndoOneEvent()
			assertTreeSitterFresh(t, b)
			b.RedoOneEvent()
			assertTreeSitterFresh(t, b)
		}
	}
}

func TestTreeSitterBatchedReplacementAndSharedView(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("package main\nfunc a() { foo() }\nfunc b() { foo() }\n", "incremental-shared.go", BTDefault)
	defer b.Close()
	view := NewBufferFromString("ignored", "incremental-shared.go", BTDefault)
	if view.SharedBuffer != b.SharedBuffer {
		t.Fatal("test did not create shared document")
	}
	h := b.Highlighter
	view.Close()
	if b.Highlighter != h {
		t.Fatal("closing a view released shared highlighter")
	}
	// The replacement path emits remove+insert operations within one text event.
	b.MultipleReplace([]Delta{
		{Start: Loc{11, 1}, End: Loc{14, 1}, Text: []byte("len")},
		{Start: Loc{11, 2}, End: Loc{14, 2}, Text: []byte("cap")},
	})
	assertTreeSitterFresh(t, b)
	b.UndoOneEvent()
	assertTreeSitterFresh(t, b)
	b.RedoOneEvent()
	assertTreeSitterFresh(t, b)
	b.UpdateRules()
	if b.Highlighter != h {
		t.Fatal("rules/theme refresh replaced persistent highlighter")
	}
	assertTreeSitterFresh(t, b)
}

func BenchmarkTreeSitterBufferEdit(b *testing.B) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	doc := NewBufferFromString("package main\n"+strings.Repeat("func f() { println(123) }\n", 400), "incremental-bench.go", BTDefault)
	defer doc.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc.Insert(Loc{0, 200}, " ")
		doc.Remove(Loc{0, 200}, Loc{1, 200})
		doc.EnsureHighlights(180, 220)
	}
}

func TestTreeSitterToggleWhileRegexRefreshQueued(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = false
	defer func() { config.GlobalSettings["treesitter"] = old }()
	doc := NewBufferFromString("package main\nfunc f() { foo() }\n", "incremental-toggle.go", BTDefault)
	defer doc.Close()
	for i := 0; i < 12; i++ {
		if err := doc.SetOption("treesitter", "true"); err != nil {
			t.Fatal(err)
		}
		doc.Insert(Loc{0, 1}, " ")
		assertTreeSitterFresh(t, doc)
		if err := doc.SetOption("treesitter", "false"); err != nil {
			t.Fatal(err)
		}
		doc.Remove(Loc{0, 1}, Loc{1, 1})
		assertTreeSitterFresh(t, doc)
	}
	if err := doc.SetOption("treesitter", "true"); err != nil {
		t.Fatal(err)
	}
	doc.ApplyDiff("package main\n/* replacement */\nfunc g() { println(42) }\n")
	assertTreeSitterFresh(t, doc)
}

func TestTreeSitterViewportFallback(t *testing.T) {
	old := config.GlobalSettings["treesitter"]
	config.GlobalSettings["treesitter"] = true
	defer func() { config.GlobalSettings["treesitter"] = old }()
	b := NewBufferFromString("package main\nfunc f() {}\n//"+strings.Repeat("x", 256*1024), "viewport-fallback.go", BTDefault)
	defer b.Close()
	b.EnsureHighlights(1, 1)
	if b.Highlighter.TreeSitterActive() || b.syntaxNeedsRefresh {
		t.Fatal("oversize file did not settle on regex")
	}
	assertTreeSitterFresh(t, b)
	m := reflect.ValueOf(b.Match(1)).Pointer()
	b.EnsureHighlights(1, 1)
	if reflect.ValueOf(b.Match(1)).Pointer() != m {
		t.Fatal("unchanged fallback retried highlighting")
	}
	b.Remove(Loc{2, 2}, b.End())
	b.EnsureHighlights(2, 2)
	if !b.Highlighter.TreeSitterActive() {
		t.Fatal("shrinking file did not recover native backend")
	}
	b.EnsureHighlights(1, 1)
	if b.Match(1)[5].Fallback() != highlight.Groups["identifier"] {
		t.Fatal("scroll after recovery kept regex colours")
	}
}
