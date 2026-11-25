; Small fixture for editor edit, viewport and capture-style tests.
"package" @preproc
(function_declaration name: (identifier) @function)
(call_expression function: (identifier) @function)
((call_expression function: (identifier) @function.builtin)
 (#eq? @function.builtin "len"))
((call_expression function: (identifier) @special)
 (#eq? @special "println"))
[(interpreted_string_literal) (raw_string_literal)] @string
(escape_sequence) @escape
(comment) @comment
