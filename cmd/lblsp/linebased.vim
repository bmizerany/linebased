" Vim syntax file
" Language:    linebased
" Maintainer:  Blake Mizerany
" Filenames:   *.linebased
" URL:         https://github.com/bmizerany/linebased

if exists("b:current_syntax")
  finish
endif

" Comments start with # at the beginning of a line
syn match linebasedComment "^#.*$"

" Variable references: $name or ${name}
syn match linebasedVariable "\$\w\+"
syn match linebasedVariable "\${[^}]\+}"

" Continuation lines (body of any expression) - just text with variables
" Note: LSP semantic tokens provide context-aware highlighting for template bodies
syn match linebasedContinuation "^\t.*$" contains=linebasedVariable

" Builtin commands: define and include
syn match linebasedDefine "^define\>" nextgroup=linebasedTemplateName skipwhite
syn match linebasedInclude "^include\>"

" Template name after define
syn match linebasedTemplateName "\S\+" contained nextgroup=linebasedParameter skipwhite

" Parameters in define line
syn match linebasedParameter "\S\+" contained nextgroup=linebasedParameter skipwhite

" Command name (first word on a non-continuation, non-comment, non-blank line)
syn match linebasedCommand "^\S\+" contains=linebasedDefine,linebasedInclude

" Highlighting links
hi def link linebasedComment Comment
hi def link linebasedContinuation Normal
hi def link linebasedDefine Keyword
hi def link linebasedInclude Keyword
hi def link linebasedTemplateName Function
hi def link linebasedParameter Identifier
hi def link linebasedVariable Special
hi def link linebasedCommand Statement

let b:current_syntax = "linebased"
