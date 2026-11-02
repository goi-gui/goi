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

["import" "as" "param" "data"] @keyword

;; Block tags: {#if}, {:else}, {/if}, {#each}, {/each}. They read as control
;; flow, so they take the keyword colour rather than the punctuation one.
[
  "{#if"
  "{:else"
  "{/if}"
  "{#each"
  "{/each}"
  "if"
  "as"
] @keyword

;; $children is a slot, not a value.
(children_slot
  "$" @punctuation.special
  (slot_name) @keyword)

;; The row variable of {#each data.rows as row}.
(each_block
  item: (identifier) @variable)

;; A declared data variable reads as a field; its type as a type.
(data_field
  name: (identifier) @property
  type: (data_type
    element: (identifier) @type))

(data_shape
  name: (identifier) @type)

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
