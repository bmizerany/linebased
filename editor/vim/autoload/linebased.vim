" Autoload functions for linebased plugin

function! linebased#check_linebased() abort
  if executable('linebased')
    return
  endif

  if !executable('go')
    echohl WarningMsg
    echom 'linebased: command not found and go is not installed. Please install Go first.'
    echohl None
    return
  endif

  let l:choice = confirm('linebased: command not found. Install it now?', "&Yes\n&No", 1)
  if l:choice == 1
    echom 'linebased: Installing...'
    let l:output = system('go install blake.io/linebased/cmd/linebased@latest')
    if v:shell_error
      echohl ErrorMsg
      echom 'linebased: Failed to install: ' . l:output
      echohl None
    else
      echom 'linebased: Installed successfully'
    endif
  endif
endfunction
