" Filetype plugin for linebased files
" Provides LSP integration for Neovim; syntax highlighting works in both

if exists('b:did_ftplugin')
  finish
endif
let b:did_ftplugin = 1

" LSP support (Neovim only)
if has('nvim')
  nnoremap <silent> <buffer> gd :lua vim.lsp.buf.definition()<CR>zz
  nnoremap <silent> <buffer> gD :lua vim.lsp.buf.type_definition()<CR>zz
  nnoremap <silent> <buffer> K :lua vim.lsp.buf.hover()<CR>

  lua << EOF
  local lblsp_path = vim.fn.exepath('lblsp')
  if lblsp_path ~= '' then
    vim.lsp.start({
      name = 'lblsp',
      cmd = { lblsp_path },
      root_dir = vim.fn.getcwd(),
    })
  end
EOF
endif
