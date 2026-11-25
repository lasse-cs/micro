//go:build treesitter && cgo

// Package tstest provides a grammar fixture for native editor tests.
package tstest

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// GoFixture is used from packages two levels below the repository root.
func GoFixture(t testing.TB) (parser, query string) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native parser loading requires Linux or macOS")
	}
	// Use the module cache, not micro's config or a locally installed plugin.
	data, err := exec.Command("go", "mod", "download", "-json", "github.com/tree-sitter/tree-sitter-go@v0.25.0").CombinedOutput()
	if err != nil {
		t.Fatalf("fetch test grammar: %v: %s", err, data)
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(data, &mod); err != nil {
		t.Fatal(err)
	}
	cc, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatal(err)
	}
	link := "-shared"
	if runtime.GOOS == "darwin" {
		link = "-dynamiclib"
	}
	parser = filepath.Join(t.TempDir(), "go.so")
	compiler := strings.Fields(string(cc))
	args := append(compiler[1:], link, "-fPIC", filepath.Join(mod.Dir, "src", "parser.c"), "-o", parser)
	if output, err := exec.Command(compiler[0], args...).CombinedOutput(); err != nil {
		t.Fatalf("compile test grammar: %v: %s", err, output)
	}
	query, err = filepath.Abs("../../internal/tstest/testdata/go.scm")
	if err != nil {
		t.Fatal(err)
	}
	return parser, query
}
