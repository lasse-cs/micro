//go:build treesitter && cgo

package highlight

import (
	"fmt"
	"strings"
	"time"

	ts "github.com/tree-sitter/go-tree-sitter"
)

const htmlInjections = `
((script_element (raw_text) @injection.content) (#set! injection.language "javascript"))
((style_element (raw_text) @injection.content) (#set! injection.language "css"))
`

func (l *registeredLanguage) configureInjections(path string) error {
	var source string
	if path != "" {
		data, err := readQuery(path)
		if err != nil {
			return err
		}
		source = string(data)
	} else if l.name == "html" {
		source = htmlInjections
	} else {
		return nil
	}
	q, err := ts.NewQuery(l.language, source)
	if err != nil {
		// Alternative HTML grammars need not use the standard raw_text nodes.
		if path == "" {
			return nil
		}
		return err
	}
	defer func() {
		if l.injections == nil {
			q.Close()
		}
	}()
	found := false
	for i, name := range q.CaptureNames() {
		switch {
		case name == "injection.content":
			l.content, found = uint(i), true
		case !strings.HasPrefix(name, "_"):
			return fmt.Errorf("unsupported capture @%s", name)
		}
	}
	if !found && q.PatternCount() > 0 {
		return fmt.Errorf("missing @injection.content")
	}
	for i := uint(0); i < q.PatternCount(); i++ {
		if q.CaptureQuantifiers(i)[l.content] == ts.CaptureQuantifierZero {
			return fmt.Errorf("pattern %d has no @injection.content", i)
		}
		settings := q.PropertySettings(i)
		if len(q.GeneralPredicates(i))+len(q.PropertyPredicates(i)) != 0 || len(settings) != 1 ||
			settings[0].Key != "injection.language" || settings[0].CaptureId != nil ||
			settings[0].Value == nil || !languageName.MatchString(*settings[0].Value) {
			return fmt.Errorf("pattern %d requires a fixed #set! injection.language", i)
		}
		l.targets = append(l.targets, *settings[0].Value)
	}
	l.injections = q
	return nil
}

type embeddedRegion struct {
	start, end int
	language   *registeredLanguage
}

func (s *treeSitterState) clearChildren() {
	for _, tree := range s.children {
		tree.Close()
	}
	s.children = nil
}

// Parse each visible island as an independent document, retaining its tree
// across viewport changes. Child query byte positions are local to the island.
func (s *treeSitterState) paintInjections(lo int, colours []Group) bool {
	hi := lo + len(colours)
	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	cursor.SetByteRange(uint(lo), uint(hi))
	cursor.SetTimeoutMicros(20_000)
	started := time.Now()
	matches := cursor.Matches(s.language.injections, s.tree.RootNode(), s.source)
	for m := matches.Next(); m != nil; m = matches.Next() {
		if time.Since(started) >= 20*time.Millisecond {
			return false
		}
		languageRegistry.RLock()
		child := languageRegistry.names[s.language.targets[m.PatternIndex]]
		languageRegistry.RUnlock()
		if child == nil {
			continue
		}
		for _, c := range m.Captures {
			if uint(c.Index) != s.language.content {
				continue
			}
			start, end := int(c.Node.StartByte()), int(c.Node.EndByte())
			from, to := max(lo, start), min(hi, end)
			if from >= to {
				continue
			}
			region := embeddedRegion{start, end, child}
			source := s.source[start:end]
			tree := s.children[region]
			if tree == nil {
				p := ts.NewParser()
				if err := p.SetLanguage(child.language); err != nil {
					p.Close()
					return false
				}
				p.SetTimeoutMicros(20_000)
				tree = p.Parse(source, nil)
				p.Close()
				if tree == nil {
					return false
				}
				if s.children == nil {
					s.children = make(map[embeddedRegion]*ts.Tree)
				}
				s.children[region] = tree
			}
			if !paintTree(child, tree, source, from-start, colours[from-lo:to-lo]) {
				return false
			}
		}
	}
	return !cursor.DidExceedMatchLimit() && time.Since(started) < 20*time.Millisecond
}
