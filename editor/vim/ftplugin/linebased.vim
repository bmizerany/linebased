" Filetype plugin for linebased files
" Provides LSP integration for Neovim; syntax highlighting works in both

if exists('b:did_ftplugin')
  finish
endif
let b:did_ftplugin = 1

setlocal makeprg=lblsp\ expand\ -fullpath\ %
setlocal errorformat=%f:%l:\ %m

let b:undo_ftplugin = 'setl cms< makeprg< errorformat<'

" LSP support (Neovim only)
if has('nvim')
  " Define <Plug> mappings
  nnoremap <silent> <buffer> <Plug>(linebased-definition) <Cmd>lua vim.lsp.buf.definition()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-type-definition) <Cmd>lua vim.lsp.buf.type_definition()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-hover) <Cmd>lua vim.lsp.buf.hover()<CR>
  nnoremap <silent> <buffer> <Plug>(linebased-references) <Cmd>lua vim.lsp.buf.references()<CR>

  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-definition)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-type-definition)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-hover)'
  let b:undo_ftplugin .= '| silent! nunmap <buffer> <Plug>(linebased-references)'

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
  endif

  " Check for lblsp once per session
  if !exists('s:checked_lblsp')
    let s:checked_lblsp = 1
    call linebased#check_lblsp()
  endif

  lua << EOF
  vim.schedule(function()
    local lblsp_path = vim.fn.exepath('lblsp')
    if lblsp_path ~= '' then
      vim.lsp.start({
        name = 'lblsp',
        cmd = { lblsp_path },
        root_dir = vim.fn.getcwd(),
      })
    end
  end)
EOF
endif
