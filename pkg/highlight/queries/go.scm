; Semantic captures with legacy palette fallbacks supplied by the Go registration.
["break" "case" "continue" "default" "go" "goto" "range" "return" "fallthrough"] @keyword.control
["else" "for" "if" "switch" "select"] @keyword.conditional
["package" "import" "const" "var" "type" "func" "defer"] @keyword.declaration
["chan" "interface" "map"] @type
"struct" @type.keyword
(type_identifier) @type
(function_declaration name: (identifier) @function)
(method_declaration name: (field_identifier) @function)
(call_expression function: (identifier) @function)
(call_expression function: (selector_expression field: (field_identifier) @function))
(call_expression function: (identifier) @function.builtin
 (#match? @function.builtin "^(append|cap|close|complex|copy|delete|imag|len|make|new|panic|print|real|recover)$"))
(call_expression function: (identifier) @function.builtin.print (#eq? @function.builtin.print "println"))
["--" "-" "-=" ":=" "!" "!=" "..." "*" "*=" "/" "/=" "&" "&&" "&=" "%" "%=" "^" "^=" "+" "++" "+=" "<-" "<" "<<" "<<=" "<=" "=" "==" ">" ">=" ">>" ">>=" "|" "|=" "||" "~"] @operator
["," "." ";" ":"] @punctuation
["(" ")" "[" "]" "{" "}"] @punctuation.bracket
[(interpreted_string_literal) (raw_string_literal) (rune_literal)] @string
(escape_sequence) @escape
[(int_literal) (float_literal) (imaginary_literal)] @number
[(true) (false) (nil)] @boolean
(iota) @preproc
(comment) @comment
