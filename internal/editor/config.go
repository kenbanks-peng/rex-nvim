package editor

var configure = `
vim.g.rex_nvim = true
vim.o.laststatus = 3
vim.g.rex_nvim_cmdheight = vim.o.cmdheight
vim.api.nvim_create_autocmd('OptionSet', {
 group = vim.api.nvim_create_augroup('rex_nvim', {clear=true}),
 pattern={'cmdheight', 'laststatus'},
 callback=function()
  if vim.o.laststatus ~= 3 then vim.o.laststatus = 3 end
  if not vim.g.rex_nvim_fitting then vim.g.rex_nvim_cmdheight=vim.o.cmdheight end
 end
})
`
