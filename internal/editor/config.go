package editor

const configure = `
vim.g.rex_nvim = true
vim.g.rex_nvim_cmdheight = vim.o.cmdheight
vim.api.nvim_create_autocmd('OptionSet', {
 group = vim.api.nvim_create_augroup('rex_nvim', {clear=true}),
 pattern='cmdheight',
 callback=function()
  if not vim.g.rex_nvim_fitting then vim.g.rex_nvim_cmdheight=vim.o.cmdheight end
 end
})
`
