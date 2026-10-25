package main

import (
	ulua "github.com/micro-editor/micro/v2/internal/lua"
	"testing"
)

func TestTreeSitterLuaValidation(t *testing.T) {
	if err := ulua.L.DoString(`
 local ts = import("micro/treesitter")
 assert(type(ts.Languages()) == "table")
 local ok, err = ts.RegisterLanguage("python", {parser="x", highlights="y", filetypes={"python"}, typo=true})
 assert(ok == nil and string.find(err,"unknown language option"))
 ok, err = ts.RegisterLanguage("python", {parser="x", highlights="y", filetypes={42}})
 assert(ok == nil and string.find(err,"filetypes"))
 ok, err = ts.RegisterLanguage("python", {parser="x", highlights="y", filetypes={"python"},captures={foo=42}})
 assert(ok == nil and string.find(err,"captures"))
 `); err != nil {
		t.Fatal(err)
	}
}
