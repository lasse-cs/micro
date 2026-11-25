# Tree-sitter highlighting

Tree-sitter is an experimental, opt-in alternative to regex highlighting. Build
with Go 1.23+, a C compiler, and CGo enabled:

```sh
go generate ./runtime
CGO_ENABLED=1 go build -tags treesitter -o micro ./cmd/micro
```

In micro, use `set treesitter true` to enable it or `set treesitter false` to
restore regex highlighting. The `syntax` option must also be enabled. No grammar
is bundled. Install a language plugin that registers a parser for your
filetype, then run `./micro -treesitter true filename`. Missing grammars, including builds without this
backend, use the normal incremental regex path.

Each shared buffer retains a parse tree across edits. Highlight queries run for
visible logical lines on redraw; split views share the tree and cached results.
Files over 256 KiB of normalized source and parser/query failures use regex.
Parsing and queries each have a 20 ms budget, not a strict total redraw limit.
After a failure, an edit or configuration change permits another native attempt.
Very long wrapped lines can still be expensive: the query covers the whole
logical line, even if only part of it fits on screen.

## Registering languages from Lua

On Linux and macOS, plugins can load native parser libraries and self-contained
queries:

```lua
local ts = import("micro/treesitter")
local ok, err = ts.RegisterLanguage("python", {
    parser = "/absolute/path/python.so",
    highlights = "/absolute/path/highlights.scm",
    filetypes = {"python"},
    captures = { ["function"] = "identifier" }, -- optional fallback overrides
})
local languages = ts.Languages()
```

Registration returns `true, nil` on success or `nil, error` on failure. Paths must
be absolute regular files. Parsers must match the host architecture and supported
Tree-sitter ABI. The optional `symbol` field overrides `tree_sitter_<name>`.
Native parsers execute inside micro; load trusted libraries only. Micro does not
download or build them.

On macOS, install Apple's Command Line Tools (`xcode-select --install`) to obtain
the compiler. Build parsers as Mach-O dynamic libraries using `cc -dynamiclib`
rather than Linux's `-shared`. Both `.dylib` and `.so` filenames work with the
loader; a `.so` suffix can preserve existing plugin registration paths. Build for
the same architecture as micro (`arm64` on Apple Silicon or `x86_64` on Intel),
and rebuild Linux parser binaries before using them on a Mac.

Filetypes are micro filetype names; registration does not add filename detection
or enable the setting. Registering the same language again replaces its filetype
list, query and capture mappings, and refreshes open buffers. Failed registration
retains the previous working version. Different languages cannot own the same
filetype. Libraries and published queries remain loaded until exit; restart micro
after replacing a parser library. Query changes can be registered again directly.

The Lua API also exists in builds without Tree-sitter, where registration returns
an unavailable-backend error and `Languages()` returns an empty table.

## Themes and query compatibility

Existing themes work through legacy capture mappings, such as `@function` to
`identifier` and `@string` to `constant.string`. The optional `captures` table
overrides those fallbacks. Unmapped captures require an explicit mapping to a
known micro syntax group. Captures beginning with `_` are invisible helpers.

Themes can style captures directly:

```text
color-link treesitter.function "cyan"
color-link treesitter.function.builtin "bold brightcyan"
```

Lookup tries the exact capture style, dotted parents (`treesitter.function`, then
`treesitter`), and finally the legacy fallback. Exact styles replace rather than
merge with parent styles. Custom YAML syntax rules do not affect native results.

Queries support the binding's text predicates. Query inheritance, Neovim-specific
directives and local-variable tracking are not implemented.

## Embedded languages

With the standard `html` parser registered, `<script>` bodies use the registered
`javascript` language and `<style>` bodies use `css`. Tags stay highlighted by
HTML. If a child language is missing, its body keeps the host highlighting.
The defaults select by element, not by MIME type or inline attribute.

Plugins can set `injections` to an absolute query file path in `RegisterLanguage`
to define their own rules (replacing the defaults for HTML). For example:

```scheme
((style_element (raw_text) @injection.content)
 (#set! injection.language "css"))
```

Each captured node is parsed independently. Targets are registered language
names, not filetypes. Only one level of fixed-language injections is supported.
