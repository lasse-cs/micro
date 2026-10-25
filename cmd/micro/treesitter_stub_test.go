//go:build !treesitter || !cgo

package main

import (
	ulua "github.com/micro-editor/micro/v2/internal/lua"
	"testing"
)

func TestTreeSitterUnavailableAPI(t *testing.T) {
	if err := ulua.L.DoString(`
 local ts=import("micro/treesitter")
 assert(#ts.Languages()==0)
 local ok,err=ts.RegisterLanguage("python",{parser="/unused/python.so",highlights="/unused/python.scm",filetypes={"python"}})
 assert(ok==nil and string.find(err,"backend unavailable"))
 `); err != nil {
		t.Fatal(err)
	}
}
