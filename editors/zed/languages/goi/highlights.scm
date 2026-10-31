;; Highlights for .goi design files.
;;
;; The language is markup: element names, property names and small value
;; expressions. Elements read as types, properties as fields, and the enum and
;; colour-role identifiers in value position as constants, because that is what
;; they are: names the framework resolves, not variables.

(comment) @comment

(escape_sequence) @string.escape
(string) @string
(color) @string.special
(number) @number
(boolean) @boolean

["import" "as" "param"] @keyword

(conditional_question) @operator
(conditional_colon) @operator
(percent_sign) @operator

(element
  name: (element_name) @type)

(property
  name: (identifier) @property)

(map_entry
  name: (identifier) @attribute)

(param_declaration
  name: (identifier) @variable.parameter)

(parameter
  "$" @punctuation.special
  (identifier) @variable.parameter)

(reference
  object: (identifier) @variable)

(reference
  "." @punctuation.delimiter
  member: (identifier) @property)

;; Enum and colour-role names: `filled`, `primary`, `dark_mode`.
(primary
  (identifier) @constant)

[
  "||"
  "&&"
  "=="
  "!="
  "<"
  "<="
  ">"
  ">="
  "+"
  "-"
  "*"
  "/"
  "!"
] @operator

[
  "{"
  "}"
  "("
  ")"
] @punctuation.bracket

[
  ","
  ";"
] @punctuation.delimiter
