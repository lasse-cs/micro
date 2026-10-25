//go:build !treesitter || !cgo

package highlight

import "fmt"

type treeSitterState struct{}

func TreeSitterAvailable(filetype string) bool { return false }

func initTreeSitterGroups()                                                                         {}
func (h *Highlighter) highlightTreeSitter(input LineStates, first, last int) bool                   { return false }
func (h *Highlighter) EditTreeSitter(startRow, startColumn, endRow, endColumn int, inserted []byte) {}
func (h *Highlighter) ResetTreeSitter()                                                             {}
func (h *Highlighter) RefreshTreeSitter()                                                           {}

func RegisterTreeSitterLanguage(spec TreeSitterLanguageSpec) error {
	return fmt.Errorf("Tree-sitter backend unavailable: build with CGO_ENABLED=1 and -tags treesitter")
}
func TreeSitterLanguages() []string { return nil }
