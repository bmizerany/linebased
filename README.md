# linebased

[![Go Reference](https://pkg.go.dev/badge/blake.io/linebased.svg)](https://pkg.go.dev/blake.io/linebased)

A Go package for parsing and expanding line-based scripts.

```
echo hello world
define greet name
	echo Hello, $name!
greet Alice
```

Linebased provides both a parser and tooling for line-based DSLs. The parser
extracts commands and their bodies with no quoting or escaping rules. The
tooling includes an LSP server for editor integration.

## Design

Linebased scripts are deliberately simple. There are no string escapes, no
quoting rules, no operator precedence, no reserved words. A line is a command
name followed by whatever text you want. That's it.

This simplicity has a cost: you can't nest expressions or compute values inline.
But it has a benefit: scripts are trivial to read, write, and debug. The parser
does exactly what you expect because there's almost nothing it can do.

Multi-line bodies use tabs for continuation. Tabs are visible, unambiguous, and
easy to type. Spaces at line start are a syntax error, which catches a common
class of invisible bugs.

Templates exist because repetition is error-prone. Parameter substitution is
the only form of abstraction provided. Templates cannot recurse, cannot redefine
themselves, and cannot produce new definitions. These restrictions prevent the
language from becoming a programming language.

The LSP server provides diagnostics, hover, and jump-to-definition. Editor
support matters.

## Example

```
# Define a reusable template
define greet name
	echo Hello, $name!

# Use it
greet Alice
greet Bob

# Include shared definitions
include helpers.lb
```

## Documentation

See the [package documentation](https://pkg.go.dev/blake.io/linebased) for
syntax, semantics, and API reference.

## Editor support

Install the LSP server:

```
go install blake.io/linebased/cmd/lblsp@latest
```

Configure Neovim:

```lua
local lspconfig = require("lspconfig")
local configs = require("lspconfig.configs")

if not configs.lblsp then
  configs.lblsp = {
    default_config = {
      cmd = { "lblsp" },
      filetypes = { "lb" },
    },
  }
end

lspconfig.lblsp.setup({})
```

You'll get diagnostics, hovers, and jump-to-definition for `.lb` files.
