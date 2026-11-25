//go:build treesitter && cgo

package highlight

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"
)

type registeredLanguage struct {
	name       string
	language   *ts.Language
	query      *ts.Query
	injections *ts.Query
	targets    []string // Indexed by injection query pattern.
	content    uint     // @injection.content capture index.
	groups     []Group
	hidden     []bool
}

var languageRegistry = struct {
	sync.RWMutex
	filetypes map[string]*registeredLanguage
	names     map[string]*registeredLanguage
	// Immutable registrations and queries live until process exit, including old
	// versions still referenced by open buffers. Registration is explicit, not per-edit.
	versions []*registeredLanguage
}{filetypes: make(map[string]*registeredLanguage), names: make(map[string]*registeredLanguage)}
var languageName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var treeSitterGroups map[string]Group

func absoluteFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %q", path)
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	return path, nil
}

var defaultCaptures = map[string]string{
	"variable": "default", "property": "default", "attribute": "identifier",
	"function": "identifier", "function.builtin": "preproc", "method": "identifier",
	"constructor": "type", "type": "type", "keyword": "statement",
	"string": "constant.string", "escape": "constant.specialChar", "string.escape": "constant.specialChar",
	"number": "constant.number", "float": "constant.number", "boolean": "constant.bool",
	"constant": "constant", "constant.builtin": "constant.bool",
	"comment": "comment", "operator": "symbol.operator",
	"punctuation": "symbol", "punctuation.bracket": "symbol.brackets",
	"tag": "symbol.tag", "embedded": "default",
}

func captureGroups(query *ts.Query, overrides map[string]string) ([]Group, error) {
	mapping := make(map[string]string, len(defaultCaptures)+len(overrides))
	for k, v := range defaultCaptures {
		mapping[k] = v
	}
	for k, v := range overrides {
		if _, ok := treeSitterGroups[v]; !ok {
			return nil, fmt.Errorf("unknown micro colour group %q", v)
		}
		mapping[k] = v
	}
	groups := make([]Group, len(query.CaptureNames()))
	for i, name := range query.CaptureNames() {
		if strings.HasPrefix(name, "_") {
			continue
		}
		// Exact overrides take priority; exact micro group names work directly.
		target, ok := overrides[name]
		if !ok {
			if g, known := treeSitterGroups[name]; known {
				groups[i] = newCaptureGroup(name, g)
				continue
			}
		}
		if !ok {
			for key := name; ; {
				if target, ok = mapping[key]; ok {
					break
				}
				dot := strings.LastIndexByte(key, '.')
				if dot < 0 {
					break
				}
				key = key[:dot]
			}
		}
		if !ok {
			return nil, fmt.Errorf("unmapped capture @%s; add a captures entry", name)
		}
		groups[i] = newCaptureGroup(name, treeSitterGroups[target])
	}
	return groups, nil
}

// RegisterTreeSitterLanguage validates everything before publishing an immutable
// registration. Failed reloads leave the previous working registration intact.
func RegisterTreeSitterLanguage(spec TreeSitterLanguageSpec) error {
	if !languageName.MatchString(spec.Name) {
		return fmt.Errorf("invalid language name %q", spec.Name)
	}
	if len(spec.Filetypes) == 0 {
		return fmt.Errorf("at least one filetype is required")
	}
	for _, ft := range spec.Filetypes {
		if ft == "" || strings.TrimSpace(ft) != ft {
			return fmt.Errorf("invalid filetype %q", ft)
		}
	}
	symbol := spec.Symbol
	if symbol == "" {
		symbol = "tree_sitter_" + spec.Name
	}
	if !languageName.MatchString(symbol) {
		return fmt.Errorf("invalid parser symbol %q", symbol)
	}
	source, err := readQuery(spec.Highlights)
	if err != nil {
		return fmt.Errorf("highlight query: %w", err)
	}
	parserPath, err := absoluteFile(spec.Parser)
	if err != nil {
		return fmt.Errorf("parser: %w", err)
	}
	language, err := loadNativeLanguage(parserPath, symbol)
	if err != nil {
		return err
	}
	parser := ts.NewParser()
	err = parser.SetLanguage(language)
	parser.Close()
	if err != nil {
		return fmt.Errorf("incompatible parser ABI: %w", err)
	}
	query, qerr := ts.NewQuery(language, string(source))
	if qerr != nil {
		return fmt.Errorf("highlight query: %v", qerr)
	}
	keep := false
	defer func() {
		if !keep {
			query.Close()
		}
	}()
	// Neovim-specific directives/locals are not silently ignored.
	for i := uint(0); i < query.PatternCount(); i++ {
		if len(query.GeneralPredicates(i)) > 0 || len(query.PropertyPredicates(i)) > 0 || len(query.PropertySettings(i)) > 0 {
			return fmt.Errorf("query pattern %d uses unsupported predicates/directives", i)
		}
	}
	groups, err := captureGroups(query, spec.Captures)
	if err != nil {
		return err
	}
	reg := &registeredLanguage{name: spec.Name, language: language, query: query, groups: groups}
	if err := reg.configureInjections(spec.Injections); err != nil {
		return fmt.Errorf("injection query: %w", err)
	}
	defer func() {
		if !keep && reg.injections != nil {
			reg.injections.Close()
		}
	}()
	reg.hidden = make([]bool, len(query.CaptureNames()))
	for i, name := range query.CaptureNames() {
		reg.hidden[i] = strings.HasPrefix(name, "_")
	}
	languageRegistry.Lock()
	defer languageRegistry.Unlock()
	for _, ft := range spec.Filetypes {
		if old := languageRegistry.filetypes[ft]; old != nil && old.name != spec.Name {
			return fmt.Errorf("filetype %q is already registered to %s", ft, old.name)
		}
	}
	for ft, old := range languageRegistry.filetypes {
		if old.name == spec.Name {
			delete(languageRegistry.filetypes, ft)
		}
	}
	for _, ft := range spec.Filetypes {
		languageRegistry.filetypes[ft] = reg
	}
	languageRegistry.versions = append(languageRegistry.versions, reg)
	languageRegistry.names[spec.Name] = reg
	keep = true
	return nil
}

func readQuery(path string) ([]byte, error) {
	path, err := absoluteFile(path)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if stat.Size() > 1024*1024 {
		return nil, fmt.Errorf("query exceeds 1 MiB")
	}
	source, err := os.ReadFile(path)
	if err == nil && regexp.MustCompile(`(?m)^;+\s*(inherits:|extends\b)`).Match(source) {
		return nil, fmt.Errorf("query inheritance/extends modelines are not supported; supply a self-contained query")
	}
	return source, err
}

func TreeSitterLanguages() []string {
	languageRegistry.RLock()
	defer languageRegistry.RUnlock()
	names := make(map[string]bool)
	for _, reg := range languageRegistry.filetypes {
		names[reg.name] = true
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// TreeSitterAvailable reports whether this build has a usable grammar and query
// registered for filetype.
func TreeSitterAvailable(filetype string) bool {
	return lookupLanguage(filetype) != nil
}

func lookupLanguage(filetype string) *registeredLanguage {
	languageRegistry.RLock()
	defer languageRegistry.RUnlock()
	return languageRegistry.filetypes[filetype]
}
