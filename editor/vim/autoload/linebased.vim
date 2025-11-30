" Autoload functions for linebased plugin

function! linebased#check_lblsp() abort
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
