" Plugin initialization for linebased
" Auto-installs lblsp if not found

if exists('g:loaded_linebased')
  finish
endif
let g:loaded_linebased = 1

" Check if lblsp is installed, offer to install if not
function! s:CheckLblsp() abort
  if executable('lblsp')
    return
  endif

  if !executable('go')
    echohl WarningMsg
    echom 'linebased: lblsp not found and go is not installed. Please install Go first.'
    echohl None
    return
  endif

  let l:choice = confirm('linebased: lblsp not found. Install it now?', "&Yes\n&No", 1)
  if l:choice == 1
    echom 'linebased: Installing lblsp...'
    let l:output = system('go install blake.io/linebased/cmd/lblsp@latest')
    if v:shell_error
      echohl ErrorMsg
      echom 'linebased: Failed to install lblsp: ' . l:output
      echohl None
    else
      echom 'linebased: lblsp installed successfully'
    endif
  endif
endfunction

" Run check on VimEnter to avoid blocking startup
autocmd VimEnter * call s:CheckLblsp()
