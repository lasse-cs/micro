package highlight

// TreeSitterLanguageSpec registers an explicitly trusted native parser and query.
// Paths must be absolute. Filetypes refer to micro's existing filetype names.
type TreeSitterLanguageSpec struct {
	Name       string
	Parser     string
	Symbol     string
	Highlights string
	Injections string // Optional path to a fixed-language injection query.
	Filetypes  []string
	Captures   map[string]string
}
