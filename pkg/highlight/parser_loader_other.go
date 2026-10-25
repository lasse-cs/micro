//go:build treesitter && cgo && !linux && !darwin

package highlight

import (
	"fmt"
	ts "github.com/tree-sitter/go-tree-sitter"
)

func loadNativeLanguage(path, symbol string) (*ts.Language, error) {
	return nil, fmt.Errorf("loadable Tree-sitter parsers are currently supported on Linux and macOS only")
}
