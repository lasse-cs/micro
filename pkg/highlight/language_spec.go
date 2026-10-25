package highlight

// TreeSitterLanguageSpec registers an explicitly trusted native parser and query.
// Paths must be absolute. Filetypes refer to micro's existing filetype names.
type TreeSitterLanguageSpec struct {
	Name       string
	Parser     string
	Symbol     string
	Highlights string
	Filetypes  []string
	Captures   map[string]string
}
