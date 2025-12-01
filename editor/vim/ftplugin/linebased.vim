" Filetype plugin for linebased files
" Provides LSP integration for Neovim; syntax highlighting works in both

if exists('b:did_ftplugin')
  finish
endif
let b:did_ftplugin = 1

setlocal makeprg=linebased\ expand\ -l\ %
setlocal errorformat=%f:%l:\ %m

let b:undo_ftplugin = 'setl cms< makeprg< errorformat<'

" LSP support (Neovim only)
if has('nvim')
  " Define <Plug> mappings
  nnoremap <silent> <buffer> <Plug>(linebased-definition) <Cmd>lua vim.lsp.buf.definition()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-type-definition) <Cmd>lua vim.lsp.buf.type_definition()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-hover) <Cmd>lua vim.lsp.buf.hover()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-references) <Cmd>lua vim.lsp.buf.references()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-inline) <Cmd>lua vim.lsp.buf.code_action({ filter = function(a) return a.kind == 'refactor.inline' end, apply = true })<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-rename) <Cmd>lua vim.lsp.buf.rename()<CR>

  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-definition)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-type-definition)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-hover)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-references)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-inline)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-rename)'

  " Set default mappings unless user opted out or already mapped
  if !exists('g:linebased_no_mappings') || !g:linebased_no_mappings
    if !hasmapto('<Plug>(linebased-definition)')
      nmap <buffer> gd <Plug>(linebased-definition)
      let b:undo_ftplugin .= '| silent! nunmap <buffer> gd'
    endif
    if !hasmapto('<Plug>(linebased-type-definition)')
      nmap <buffer> gD <Plug>(linebased-type-definition)
      let b:undo_ftplugin .= '| silent! nunmap <buffer> gD'
    endif
    if !hasmapto('<Plug>(linebased-hover)')
      nmap <buffer> K <Plug>(linebased-hover)
      let b:undo_ftplugin .= '| silent! nunmap <buffer> K'
    endif
    if !hasmapto('<Plug>(linebased-inline)')
      nmap <buffer> gri <Plug>(linebased-inline)
      let b:undo_ftplugin .= '| silent! nunmap <buffer> gri'
    endif
    if !hasmapto('<Plug>(linebased-rename)')
      nmap <buffer> rn <Plug>(linebased-rename)
      let b:undo_ftplugin .= '| silent! nunmap <buffer> rn'
    endif
  endif

  " Check for linebased command once per session
  if !exists('s:checked_linebased')
    let s:checked_linebased = 1
    call linebased#check_linebased()
  endif

  lua << EOF
  vim.schedule(function()
    local linebased_path = vim.fn.exepath('linebased')
    if linebased_path ~= '' then
      vim.lsp.start({
        name = 'linebased',
        cmd = { linebased_path, 'lsp' },
        root_dir = vim.fn.getcwd(),
      })
    end
  end)
EOF
endif
