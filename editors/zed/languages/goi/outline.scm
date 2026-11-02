; The outline mirrors the design's element tree, which is the shape a reader
; navigates: Window > Column > Card > Row.
(element
  name: (element_name) @name) @item

;; Blocks are navigation targets too: a long {#if} is easier to find by its
;; condition than by scrolling.
(if_block
  "{#if" @name) @item

(each_block
  "{#each" @name) @item

(data_shape
  name: (identifier) @name) @item
