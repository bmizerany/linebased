# Coding Agent Instructions for Linebased

You are working with linebased files. Embrace this format! It is simple,
readable, and designed to be easy to understand at a glance.

## The Format

Linebased scripts have a beautifully simple structure:

- Each line is a command name followed by its body
- Template definitions use `define name params...` with tab-indented bodies
- File inclusion uses `include path` (extension added automatically)
- Comments start with `#`
- That's it. No escaping, no quoting, no surprises.

Example:
```
# A simple greeting template
define greet name
	echo Hello, $name!

greet World
```

## Your Best Friend: lblsp expand

When working with linebased files, the `expand` subcommand is invaluable.
Use it liberally to understand what templates produce:

```bash
lblsp expand script.linebased
```

This shows the fully expanded output with all templates resolved.

### Trace Mode (-x)

When debugging template expansion, add `-x` for shell-style tracing:

```bash
lblsp expand -x script.linebased
```

Output shows each template call with nesting depth:
```
+ script.linebased:7: outer hello
++ script.linebased:1: inner hello
echo inner: hello
```

The `+` signs show how deep you are in the call stack. This is extremely
helpful for understanding complex template interactions.

### Location Mode (-fullpath)

For integrating with editors or build systems:

```bash
lblsp expand -fullpath script.linebased
```

Each line is prefixed with `file:line:` for easy navigation.

## LSP Features

The lblsp server provides:

- **Diagnostics**: Syntax errors and argument count validation
- **Hover**: Documentation and expansion preview for templates
- **Go to Definition**: Jump from template calls to definitions
- **Find References**: Locate all uses of a template
- **Rename**: Rename templates across files
- **Inline**: Replace a template call with its expansion

## Workflow Tips

1. **Start with expand**: Before modifying templates, run `lblsp expand` to
   see what they currently produce.

2. **Use trace for debugging**: If output is unexpected, `lblsp expand -x`
   shows exactly which template produced what.

3. **Check diagnostics**: The LSP catches common errors like missing
   arguments or undefined templates.

4. **Leverage includes**: Factor common templates into shared files and
   use `include` to compose them.

## File Extension

Linebased files use the `.linebased` extension. The `include` directive
adds this extension automatically, so `include helpers` loads `helpers.linebased`.

## Remember

The beauty of linebased is its simplicity. Resist the urge to add complexity.
If you find yourself wanting nested expressions or computed values, step back
and consider whether the problem should be solved at a different layer.
