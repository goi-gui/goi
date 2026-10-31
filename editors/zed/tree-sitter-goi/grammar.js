/**
 * Tree-sitter grammar for goi design files (.goi).
 *
 * The language is markup, not a script: elements hold properties, values are
 * literals or small expressions, and behaviour lives in Go. Newlines are not
 * tokens, so a member is a property when its identifier is followed by `:` and
 * a child element when it is followed by `{`.
 */
module.exports = grammar({
  name: 'goi',

  extras: $ => [/\s/, $.comment],

  word: $ => $.identifier,

  rules: {
    source_file: $ => seq(
      repeat(choice($.import_statement, $.param_declaration)),
      field('root', $.element),
    ),

    // import "./Sidebar.goi" as Sidebar
    import_statement: $ => seq(
      'import',
      field('path', $.string),
      'as',
      field('alias', $.identifier),
      optional(';'),
    ),

    // param title: "Mail"
    param_declaration: $ => seq(
      'param',
      field('name', $.identifier),
      ':',
      field('value', $.value),
      optional(';'),
    ),

    // Column { ... }
    element: $ => seq(
      field('name', $.element_name),
      '{',
      repeat($._member),
      '}',
    ),

    _member: $ => choice($.property, $.element),

    // spacing: 8
    property: $ => seq(
      field('name', $.identifier),
      ':',
      field('value', $.value),
      optional(';'),
    ),

    element_name: _ => /[A-Z][A-Za-z0-9_]*/,

    // Values are one expression grammar, so `text: hello` and
    // `width: side.width / 2` are both plain values.
    // A value is an expression; the conditional is optional, which is what
    // makes `text: hello` a plain value and `a ? b : c` a conditional.
    value: $ => prec.right(seq(
      field('condition', $.or_expression),
      optional(seq(
        alias('?', $.conditional_question),
        field('then', $.value),
        alias(':', $.conditional_colon),
        field('else', $.value),
      )),
    )),

    or_expression: $ => prec.left(seq(
      $.and_expression,
      repeat(seq('||', $.and_expression)),
    )),

    and_expression: $ => prec.left(seq(
      $.equality_expression,
      repeat(seq('&&', $.equality_expression)),
    )),

    equality_expression: $ => prec.left(seq(
      $.comparison_expression,
      repeat(seq(choice('==', '!='), $.comparison_expression)),
    )),

    comparison_expression: $ => prec.left(seq(
      $.sum_expression,
      repeat(seq(choice('<', '<=', '>', '>='), $.sum_expression)),
    )),

    sum_expression: $ => prec.left(seq(
      $.term_expression,
      repeat(seq(choice('+', '-'), $.term_expression)),
    )),

    term_expression: $ => prec.left(seq(
      $.unary_expression,
      repeat(seq(choice('*', '/'), $.unary_expression)),
    )),

    unary_expression: $ => choice(
      prec(2, seq(choice('!', '-'), $.unary_expression)),
      $.primary,
    ),

    primary: $ => choice(
      $.string,
      $.percentage,
      $.number,
      $.boolean,
      $.color,
      $.reference,
      $.parameter,
      $.identifier,
      $.map,
      $.parenthesized_expression,
    ),

    parenthesized_expression: $ => seq('(', $.value, ')'),

    // 50%
    percentage: $ => seq($.number, alias('%', $.percent_sign)),

    // side.width, app.dark, window.width
    reference: $ => prec(1, seq(
      field('object', $.identifier),
      '.',
      field('member', $.identifier),
    )),

    // $title — filled in by the instance that imports the fragment.
    parameter: $ => seq('$', $.identifier),

    // { base: 16, md: 24 }
    map: $ => seq(
      '{',
      optional(seq(
        $.map_entry,
        repeat(seq(',', $.map_entry)),
        optional(','),
      )),
      '}',
    ),

    map_entry: $ => seq(
      field('name', $.identifier),
      ':',
      field('value', $.value),
    ),

    string: $ => seq(
      '"',
      repeat(choice(
        $._string_content,
        $.escape_sequence,
      )),
      '"',
    ),

    _string_content: _ => token.immediate(/[^"\\\n]+/),

    escape_sequence: _ => token.immediate(seq(
      '\\',
      choice(
        /["\\/bfnrt]/,
        /u\{[0-9a-fA-F]{1,6}\}/,
        /u[0-9a-fA-F]{4}/,
      ),
    )),

    number: _ => {
      const digits = /[0-9]+(_[0-9]+)*/;
      const fraction = seq('.', digits);
      return token(seq(digits, optional(fraction)));
    },

    boolean: _ => choice('true', 'false'),

    // #rgb, #rrggbb, #rrggbbaa
    color: _ => token(seq(
      '#',
      choice(
        /[0-9a-fA-F]{8}/,
        /[0-9a-fA-F]{6}/,
        /[0-9a-fA-F]{3}/,
      ),
    )),

    identifier: _ => /[A-Za-z_][A-Za-z0-9_]*/,

    comment: _ => token(choice(
      seq('//', /[^\n]*/),
      seq('/*', /[^*]*\*+([^/*][^*]*\*+)*/, '/'),
    )),
  },
});
