//go:build treesitter && cgo

package display

import (
	"strings"
	"testing"

	"github.com/micro-editor/micro/v2/internal/buffer"
	"github.com/micro-editor/micro/v2/internal/config"
	ulua "github.com/micro-editor/micro/v2/internal/lua"
	"github.com/micro-editor/micro/v2/internal/screen"
	"github.com/micro-editor/micro/v2/internal/tstest"
	"github.com/micro-editor/micro/v2/pkg/highlight"
	"github.com/micro-editor/tcell/v2"
	lua "github.com/yuin/gopher-lua"
)

func TestTreeSitterViewportDisplay(t *testing.T) {
	parser, query := tstest.GoFixture(t)
	if err := highlight.RegisterTreeSitterLanguage(highlight.TreeSitterLanguageSpec{Name: "go", Filetypes: []string{"go"}, Parser: parser, Highlights: query}); err != nil {
		t.Fatal(err)
	}
	ulua.L = lua.NewState()
	defer ulua.L.Close()
	config.InitRuntimeFiles(false)
	config.InitGlobalSettings()
	config.GlobalSettings["backup"] = false
	config.GlobalSettings["treesitter"] = true
	config.GlobalSettings["statusline"] = false
	config.GlobalSettings["ruler"] = false
	config.GlobalSettings["scrollbar"] = false
	config.GlobalSettings["matchbrace"] = false
	sim, err := screen.InitSimScreen()
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Fini()
	b := buffer.NewBufferFromString("package main\n/*\n"+strings.Repeat("func f() { foo(123) }\n", 100)+"*/\n", "viewport-display.go", buffer.BTDefault)
	defer b.Close()
	if b.Highlighter.TreeSitterActive() || b.Match(40) != nil {
		t.Fatal("buffer eagerly highlighted")
	}
	a := NewBufWindow(0, 0, 40, 10, b)
	c := NewBufWindow(40, 0, 40, 10, b)
	a.StartLine = SLoc{Line: 40}
	c.StartLine = SLoc{Line: 80}
	a.Display()
	if !b.Highlighter.TreeSitterActive() || b.Match(40)[0].Fallback() != highlight.Groups["comment"] {
		t.Fatal("first viewport not prepared")
	}
	if b.Match(80) != nil || b.Match(1) != nil {
		t.Fatal("painted offscreen rows")
	}
	c.Display()
	if b.Match(80)[0].Fallback() != highlight.Groups["comment"] {
		t.Fatal("split viewport not prepared")
	}
	b.Replace(buffer.Loc{X: 0, Y: 1}, buffer.Loc{X: 2, Y: 1}, "//")
	a.Display()
	c.Display()
	if b.Match(40)[5].Fallback() != highlight.Groups["identifier"] || b.Match(80)[5].Fallback() != highlight.Groups["identifier"] {
		t.Fatal("split retained stale comment colours")
	}
	// Dedicated theme groups resolve during rendering and never affect regex.
	b.EnsureHighlights(40, 40)
	g := b.Match(40)[5]
	if g.String() != "treesitter.function" {
		t.Fatal("semantic capture name lost")
	}
	saved := config.Colorscheme
	defer func() { config.Colorscheme = saved }()
	palette, err := config.ParseColorscheme("test", `color-link identifier "yellow"
color-link treesitter.function "cyan"
color-link treesitter.attribute "magenta"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	config.Colorscheme = palette
	style, _ := a.getStyle(config.DefStyle, buffer.Loc{X: 5, Y: 40})
	if style != palette["treesitter.function"] {
		t.Fatal("renderer ignored dedicated function colour")
	}
	if config.GetSyntaxColor(highlight.Groups["identifier"]) != palette["identifier"] {
		t.Fatal("Tree-sitter style leaked into regex")
	}
	delete(palette, "treesitter.function")
	palette["treesitter"] = config.StringToStyle("green")
	if config.GetSyntaxColor(g) != palette["treesitter"] {
		t.Fatal("capture parent fallback failed")
	}
	delete(palette, "treesitter")
	if config.GetSyntaxColor(g) != palette["identifier"] {
		t.Fatal("legacy palette fallback failed")
	}
	// Theme replacement must resolve immediately, without cached old styles.
	config.Colorscheme = map[string]tcell.Style{"identifier": config.StringToStyle("red")}
	if config.GetSyntaxColor(g) != config.Colorscheme["identifier"] {
		t.Fatal("theme switch retained old colours")
	}
	// A dotted capture inherits its nearest configured parent, with exact wins.
	b.Replace(buffer.Loc{X: 11, Y: 40}, buffer.Loc{X: 14, Y: 40}, "len")
	b.EnsureHighlights(40, 40)
	builtin := b.Match(40)[11]
	if builtin.String() != "treesitter.function.builtin" {
		t.Fatal("builtin capture missing")
	}
	config.Colorscheme = map[string]tcell.Style{"treesitter.function": config.StringToStyle("cyan")}
	if config.GetSyntaxColor(builtin) != config.Colorscheme["treesitter.function"] {
		t.Fatal("dotted parent fallback failed")
	}
	config.Colorscheme["treesitter.function.builtin"] = config.StringToStyle("magenta")
	if config.GetSyntaxColor(builtin) != config.Colorscheme["treesitter.function.builtin"] {
		t.Fatal("exact capture did not win")
	}
	config.Colorscheme = saved
	// Scroll into a previously unvisited region.
	a.StartLine = SLoc{Line: 60}
	a.Display()
	if b.Match(60)[5].Fallback() != highlight.Groups["identifier"] {
		t.Fatal("scroll retained stale colours")
	}
	// A narrow wrapped view must include the last logical line it draws.
	b.Settings["softwrap"] = true
	b.Settings["wordwrap"] = true
	a.Width = 12
	a.StartLine = SLoc{Line: 20, Row: 1}
	a.Display()
	last := a.Scroll(a.StartLine, a.bufHeight-1).Line
	if b.Match(last)[5].Fallback() != highlight.Groups["identifier"] {
		t.Fatal("wrapped viewport missed final line")
	}
}
