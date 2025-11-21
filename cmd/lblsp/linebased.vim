" Vim syntax file
" Language:    linebased
" Maintainer:  Blake Mizerany
" Filenames:   *.lb
" URL:         https://github.com/bmizerany/linebased

if exists("b:current_syntax")
  finish
endif

" Comments start with # at the beginning of a line
syn match linebasedComment "^#.*$"

" Continuation lines start with a tab
syn match linebasedContinuation "^\t.*$" contains=linebasedVariable

" Builtin commands: define and include
syn match linebasedDefine "^define\>" nextgroup=linebasedTemplateName skipwhite
syn match linebasedInclude "^include\>"

" Template name after define
syn match linebasedTemplateName "\S\+" contained nextgroup=linebasedParameter skipwhite

" Parameters in define line
syn match linebasedParameter "\S\+" contained nextgroup=linebasedParameter skipwhite

" Variable references: $name or ${name}
syn match linebasedVariable "\$\w\+"
syn match linebasedVariable "\${[^}]\+}"

" Command name (first word on a non-continuation, non-comment, non-blank line)
syn match linebasedCommand "^\S\+" contains=linebasedDefine,linebasedInclude

" Highlighting links
hi def link linebasedComment Comment
hi def link linebasedContinuation String
hi def link linebasedDefine Keyword
hi def link linebasedInclude Keyword
hi def link linebasedTemplateName Function
hi def link linebasedParameter Identifier
hi def link linebasedVariable Special
hi def link linebasedCommand Statement

let b:current_syntax = "linebased"
