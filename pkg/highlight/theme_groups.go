package highlight

import "sync"

// Native capture groups use a separate ID space and registry. Registration can
// run while other buffers render; it never mutates the legacy regex Groups map.
const captureGroupBase Group = 1 << 31

type captureStyle struct {
	name     string
	fallback Group
}

var captureStyles = struct {
	sync.RWMutex
	ids    map[captureStyle]Group
	styles []captureStyle
}{ids: make(map[captureStyle]Group)}

func newCaptureGroup(name string, fallback Group) Group {
	style := captureStyle{"treesitter." + name, fallback}
	captureStyles.Lock()
	defer captureStyles.Unlock()
	if id, ok := captureStyles.ids[style]; ok {
		return id
	}
	id := captureGroupBase + Group(len(captureStyles.styles))
	captureStyles.styles = append(captureStyles.styles, style)
	captureStyles.ids[style] = id
	return id
}

func (g Group) captureStyle() (captureStyle, bool) {
	if g < captureGroupBase {
		return captureStyle{}, false
	}
	captureStyles.RLock()
	defer captureStyles.RUnlock()
	i := uint64(g - captureGroupBase)
	if i >= uint64(len(captureStyles.styles)) {
		return captureStyle{}, false
	}
	return captureStyles.styles[i], true
}

// Fallback returns the legacy theme group for a Tree-sitter capture, or g itself.
func (g Group) Fallback() Group {
	if style, ok := g.captureStyle(); ok {
		return style.fallback
	}
	return g
}
