//go:build treesitter && cgo

package highlight

import (
	"bytes"
	_ "embed"
	"sort"
	"sync"
	"time"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsg "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

//go:embed queries/go.scm
var goHighlightQuery string

// Keep the prototype memory/latency cap, including cold parses and fallback.
const treeSitterMaxBytes = 256 * 1024

// The immutable query is shared for the process lifetime. Each document owns
// its parser/tree; query cursors are short lived.
var goQueryOnce sync.Once
var goQuery *ts.Query
var goLanguage = ts.NewLanguage(tsg.Language())

func compiledGoQuery() *ts.Query {
	goQueryOnce.Do(func() {
		query, err := ts.NewQuery(goLanguage, goHighlightQuery)
		if err == nil {
			goQuery = query
		}
	})
	return goQuery
}

func initTreeSitterGroups() {
	treeSitterGroups = map[string]Group{"default": 0}
	for _, name := range []string{"special", "statement", "preproc", "type", "type.keyword", "identifier", "symbol.operator", "symbol", "symbol.brackets", "constant.string", "constant.specialChar", "constant.number", "constant.bool", "comment", "constant", "todo", "error", "symbol.tag", "constant.string.char"} {
		numGroups++
		Groups[name] = numGroups
		treeSitterGroups[name] = numGroups
	}
}

// treeSitterState belongs to a Highlighter on a SharedBuffer, never to a view.
// The buffer serializes access with syntaxMu. Source is an LF-normalized mirror
// with one final sentinel newline, matching the initial prototype's input.
type treeSitterState struct {
	language                                    *registeredLanguage
	parser                                      *ts.Parser
	tree                                        *ts.Tree
	source                                      []byte
	starts                                      []int
	pending                                     bool
	valid                                       map[int]bool
	revision, parsedRevision                    uint64
	parses, incrementalParses, highlightedLines uint64
}

type treeSitterSpan struct {
	start, end uint
	pattern    uint
	group      Group
}

// ResetTreeSitter releases native resources. The active flag is intentionally
// retained until HighlightMatches has replaced every old match on fallback.
func (h *Highlighter) ResetTreeSitter() {
	if s := h.treeSitter; s != nil {
		if s.tree != nil {
			s.tree.Close()
		}
		if s.parser != nil {
			s.parser.Close()
		}
		h.treeSitter = nil
	}
}

// RefreshTreeSitter invalidates colours, not the parse tree (e.g. theme reload).
func (h *Highlighter) RefreshTreeSitter() {
	if s := h.treeSitter; s != nil {
		s.valid = nil
	}
}

// EditTreeSitter must be called before mutating the buffer, with byte columns
// in the old document and LF-normalized inserted text. It invalidates all
// cached highlight ranges, then edits the old tree.
func (h *Highlighter) EditTreeSitter(startRow, startColumn, endRow, endColumn int, inserted []byte) {
	s := h.treeSitter
	if s == nil {
		return
	}
	if !h.TreeSitter || startRow < 0 || endRow < startRow || endRow >= len(s.starts) {
		h.ResetTreeSitter()
		return
	}
	start, oldEnd := s.starts[startRow]+startColumn, s.starts[endRow]+endColumn
	lineEnd := func(row int) int {
		if row+1 < len(s.starts) {
			return s.starts[row+1] - 1
		}
		return len(s.source) - 1
	}
	if startColumn < 0 || endColumn < 0 || start > lineEnd(startRow) || oldEnd > lineEnd(endRow) || oldEnd < start {
		h.ResetTreeSitter()
		return
	}
	newEnd := start + len(inserted)
	delta := newEnd - oldEnd
	if len(s.source)+delta > treeSitterMaxBytes {
		h.ResetTreeSitter()
		return
	}
	s.pending = true
	s.valid = nil // All query results depend on this document revision.
	newPosition := ts.Point{Row: uint(startRow), Column: uint(startColumn + len(inserted))}
	if count := bytes.Count(inserted, []byte{'\n'}); count > 0 {
		newPosition.Row += uint(count)
		newPosition.Column = uint(len(inserted) - bytes.LastIndexByte(inserted, '\n') - 1)
	}
	if s.tree != nil {
		s.tree.Edit(&ts.InputEdit{StartByte: uint(start), OldEndByte: uint(oldEnd), NewEndByte: uint(newEnd),
			StartPosition:  ts.Point{Row: uint(startRow), Column: uint(startColumn)},
			OldEndPosition: ts.Point{Row: uint(endRow), Column: uint(endColumn)}, NewEndPosition: newPosition})
	}
	// Splice into the mirror. The tail moves in place when capacity permits;
	// unlike rebuilding from lines, this does not allocate/copy the whole file.
	oldLen := len(s.source)
	if delta > 0 {
		s.source = append(s.source, make([]byte, delta)...)
	}
	copy(s.source[newEnd:], s.source[oldEnd:oldLen])
	s.source = s.source[:oldLen+delta]
	copy(s.source[start:newEnd], inserted)
	// Keep a line-offset index, including the actual final empty line if present.
	// A splice shifts the suffix; indexing bytes within a row remains constant time.
	starts := make([]int, 0, len(s.starts)+int(newPosition.Row)-endRow)
	starts = append(starts, s.starts[:startRow+1]...)
	for i, c := range inserted {
		if c == '\n' {
			starts = append(starts, start+i+1)
		}
	}
	for _, pos := range s.starts[endRow+1:] {
		starts = append(starts, pos+delta)
	}
	s.starts = starts
	s.revision++
}

func newTreeSitterState(input LineStates, language *registeredLanguage) *treeSitterState {
	if input.LinesNum() == 0 {
		return nil
	}
	s := &treeSitterState{language: language}
	for i := 0; i < input.LinesNum(); i++ {
		line := input.LineBytes(i)
		if len(s.source)+len(line)+1 > treeSitterMaxBytes {
			return nil
		}
		s.starts = append(s.starts, len(s.source))
		s.source = append(s.source, line...)
		s.source = append(s.source, '\n')
	}
	s.parser = ts.NewParser()
	if s.parser.SetLanguage(language.language) != nil {
		s.parser.Close()
		return nil
	}
	s.parser.SetTimeoutMicros(20_000)
	return s
}

// highlightTreeSitter commits complete results only, reusing untouched line maps.
// Failure releases parser/tree state so the next attempt starts from fresh input.
func (h *Highlighter) highlightTreeSitter(input LineStates, first, last int) (ok bool) {
	if h.Def == nil {
		return false
	}
	input.Lock()
	defer input.Unlock()
	defer func() {
		if !ok {
			h.ResetTreeSitter()
		}
	}()
	filetype := h.TreeSitterFiletype
	if filetype == "" {
		filetype = h.Def.FileType
	}
	language := lookupLanguage(filetype)
	if language == nil {
		return false
	}
	query := language.query
	s := h.treeSitter
	if s != nil && s.language != language {
		h.ResetTreeSitter()
		s = nil
	}
	if s == nil {
		s = newTreeSitterState(input, language)
		h.treeSitter = s
	}
	if s == nil {
		return false
	}
	// Line-count mismatch means a caller missed the precise-edit contract.
	if len(s.starts) != input.LinesNum() {
		return false
	}
	if s.tree == nil || s.pending {
		old := s.tree
		// Bounded chunks prevent the Go binding from copying the entire source into
		// C on each callback. The mirror also provides text for query predicates.
		tree := s.parser.ParseWith(func(offset int, _ ts.Point) []byte {
			if offset >= len(s.source) {
				return nil
			}
			return s.source[offset:min(len(s.source), offset+4096)]
		}, old)
		if tree == nil {
			return false
		}
		s.parses++
		if old != nil {
			s.incrementalParses++
			old.Close()
		}
		s.tree = tree
		s.parsedRevision = s.revision
	}
	s.pending = false
	first = max(0, first)
	last = min(last, len(s.starts)-1)
	// Cached rows belong to one text/query revision and are shared by all views.
	// Trimming valid edges avoids repeating queries when scrolling or drawing splits.
	for first <= last && s.valid[first] {
		first++
	}
	for last >= first && s.valid[last] {
		last--
	}
	if first > last {
		return true
	}
	lo, hi := s.starts[first], len(s.source)
	if last+1 < len(s.starts) {
		hi = s.starts[last+1]
	}
	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	cursor.SetTimeoutMicros(20_000)
	cursor.SetByteRange(uint(lo), uint(hi))
	started := time.Now()
	matches := cursor.Matches(query, s.tree.RootNode(), s.source)
	var spans []treeSitterSpan
	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			if int(capture.Index) < len(language.hidden) && language.hidden[capture.Index] {
				continue
			}
			spans = append(spans, treeSitterSpan{capture.Node.StartByte(), capture.Node.EndByte(), match.PatternIndex, language.groups[capture.Index]})
		}
	}
	if cursor.DidExceedMatchLimit() || time.Since(started) >= 20*time.Millisecond {
		return false
	}
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if a.end-a.start != b.end-b.start {
			return a.end-a.start > b.end-b.start
		}
		return a.pattern < b.pattern
	})
	// Query from the root to preserve ancestor context, but paint requested rows only.
	// Captures crossing viewport boundaries are clipped after matching.
	colours := make([]Group, hi-lo)
	for _, span := range spans {
		for i := max(lo, int(span.start)); i < min(hi, int(span.end)); i++ {
			colours[i-lo] = span.group
		}
	}
	if s.valid == nil {
		s.valid = make(map[int]bool)
	}
	for row := first; row <= last; row++ {
		line := input.LineBytes(row)
		match := make(LineMatch)
		column, offset := 0, 0
		var previous Group
		for offset < len(line) {
			group := colours[s.starts[row]+offset-lo]
			if column == 0 || group != previous {
				match[column] = group
			}
			previous = group
			_, _, size := DecodeCharacter(line[offset:])
			offset += size
			column++
		}
		input.SetMatch(row, match)
		s.valid[row] = true
		s.highlightedLines++
	}
	return true
}
