; Both elements and responsive maps open a block, so a line ending in `{`
; indents the next one by a level.
(_ "{" "}" @end) @indent

; A block tag opens a level and its closing tag ends it, so the body of an
; {#if} indents the same way an element body does.
(if_block) @indent
(if_block "{/if}" @end)
(each_block) @indent
(each_block "{/each}" @end)
