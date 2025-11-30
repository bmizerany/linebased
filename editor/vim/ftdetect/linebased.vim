" Filetype detection for linebased files
augroup filetypedetect_linebased
  autocmd!
  autocmd BufRead,BufNewFile *.linebased setfiletype linebased
augroup END
