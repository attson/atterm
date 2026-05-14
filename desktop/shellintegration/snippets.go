package shellintegration

import _ "embed"

//go:embed snippets/atterm.zsh
var zshSnippet string

//go:embed snippets/atterm.bash
var bashSnippet string

//go:embed snippets/atterm.fish
var fishSnippet string

//go:embed snippets/atterm.ps1
var pwshSnippet string
