/*
Command lblsp provides tooling for linebased files: an LSP server for editor
integration and an expand subcommand for debugging template expansion.

# Installation

To install the latest version of lblsp, run:

	go install blake.io/linebased/cmd/lblsp@latest

# Expand Subcommand

The expand subcommand outputs a linebased file with all templates expanded
and includes resolved:

	lblsp expand script.linebased

Use the -x flag for shell-style tracing that shows each template call as it
expands:

	$ lblsp expand -x script.linebased
	+ script.linebased:7: outer hello
	++ script.linebased:1: inner hello
	echo inner: hello

The + signs indicate nesting depth. When outer calls inner which produces echo,
you see + for outer, ++ for inner, then the final expanded expression.

# LSP Features

When run without arguments, lblsp starts an LSP server with:

  - Diagnostics: Syntax errors and argument count validation
  - Hover: Documentation for templates at definition and call sites
  - Go to Definition: Navigate from template calls to their definitions
  - Find References: Locate all uses of a template
  - Semantic Tokens: Syntax highlighting for comments, keywords, templates,
    parameters, and variable expansions

# Editor Setup

lblsp communicates over stdin/stdout using the LSP protocol. Configure your
editor to run lblsp as the language server for .linebased files.

# Vim / Neovim

Clone the repository and add the editor/vim directory to your runtimepath.
In your vimrc or init.vim:

	set runtimepath+=~/src/linebased/editor/vim

Or in init.lua:

	vim.opt.rtp:append('~/src/linebased/editor/vim')

The plugin provides filetype detection, syntax highlighting, and LSP
integration (Neovim only).

Using coc.nvim (alternative to built-in LSP), add to coc-settings.json:

	{
		"languageserver": {
			"lblsp": {
				"command": "lblsp",
				"filetypes": ["linebased"],
				"rootPatterns": [".git/"]
			}
		}
	}

# VS Code

Create .vscode/settings.json in your workspace:

	{
		"lsp.servers": {
			"lblsp": {
				"command": "lblsp",
				"filetypes": ["linebased"]
			}
		}
	}

Or use an extension that supports custom language servers.

# Emacs

Using lsp-mode:

	(add-to-list 'lsp-language-id-configuration '(linebased-mode . "linebased"))
	(lsp-register-client
		(make-lsp-client
			:new-connection (lsp-stdio-connection '("lblsp"))
			:major-modes '(linebased-mode)
			:server-id 'lblsp))

Using eglot:

	(add-to-list 'eglot-server-programs '(linebased-mode . ("lblsp")))

# Helix

Add to languages.toml:

	[[language]]
	name = "linebased"
	scope = "source.linebased"
	file-types = ["linebased"]
	roots = []
	language-servers = ["lblsp"]

	[language-server.lblsp]
	command = "lblsp"

# Zed

Add to settings.json:

	{
		"lsp": {
			"lblsp": {
				"binary": {
					"path": "lblsp"
				}
			}
		},
		"languages": {
			"Linebased": {
				"language_servers": ["lblsp"]
			}
		}
	}
*/
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"blake.io/linebased"
)

// JSON-RPC error codes
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "expand":
			if err := runExpand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "lblsp expand: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	s := &server{
		r:    bufio.NewReader(os.Stdin),
		w:    bufio.NewWriter(os.Stdout),
		docs: make(map[string]*document),
	}
	if err := s.run(); err != nil {
		var e exitError
		if errors.As(err, &e) {
			os.Exit(e.code)
		}
		fmt.Fprintf(os.Stderr, "lblsp: %v\n", err)
		os.Exit(1)
	}
}

func runExpand(args []string) error {
	fs := flag.NewFlagSet("expand", flag.ContinueOnError)
	trace := fs.Bool("x", false, "print expansion steps to stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return errors.New("missing filename")
	}
	filename := fs.Arg(0)

	// Track last traced stack to avoid duplicate trace lines
	var lastStack []linebased.Expanded

	// Get absolute path and directory for the filesystem root
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	fsys := os.DirFS(dir)
	dec := linebased.NewExpandingDecoder(base, fsys)

	for {
		expr, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		// Output comment if present
		if expr.Comment != "" {
			fmt.Print(expr.Comment)
		}

		// In trace mode, show template calls being expanded (like sh -x)
		// Only print stack frames that differ from the last traced stack
		if *trace {
			for i, caller := range expr.Stack {
				// Skip if this frame matches the last traced stack
				if i < len(lastStack) && lastStack[i].Name == caller.Name && lastStack[i].Line == caller.Line {
					continue
				}
				prefix := strings.Repeat("+", i+1)
				fmt.Fprintf(os.Stderr, "%s %s:%d: %s\n", prefix, caller.File, caller.Line, strings.TrimSuffix(caller.String(), "\n"))
			}
			lastStack = expr.Stack
		}

		// Output expression (including blank lines to preserve structure)
		fmt.Print(expr.String())
	}

	return nil
}

// Server

type server struct {
	r        *bufio.Reader
	w        *bufio.Writer
	docs     map[string]*document
	shutdown bool
}

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func (s *server) run() error {
	for {
		data, err := s.readMessage()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var msg request
		if err := json.Unmarshal(data, &msg); err != nil {
			s.sendError(nil, codeParseError, err.Error())
			continue
		}
		if err := s.dispatch(&msg); err != nil {
			return err
		}
	}
}

func (s *server) dispatch(msg *request) error {
	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "initialized":
		return nil
	case "shutdown":
		return s.handleShutdown(msg)
	case "exit":
		return s.handleExit()
	case "textDocument/didOpen":
		return s.handleDidOpen(msg)
	case "textDocument/didChange":
		return s.handleDidChange(msg)
	case "textDocument/didClose":
		return s.handleDidClose(msg)
	case "textDocument/hover":
		return s.handleHover(msg)
	case "textDocument/definition":
		return s.handleDefinition(msg)
	case "textDocument/references":
		return s.handleReferences(msg)
	case "textDocument/semanticTokens/full":
		return s.handleSemanticTokens(msg)
	case "$/cancelRequest", "workspace/didChangeConfiguration":
		return nil
	default:
		if msg.ID != nil {
			return s.sendError(msg.ID, codeMethodNotFound, fmt.Sprintf("unsupported method %q", msg.Method))
		}
		return nil
	}
}

// Handlers

func (s *server) handleInitialize(msg *request) error {
	// Static response - capabilities don't change
	// Token types:
	//   string - template body lines
	//   variable - $VAR/${VAR} expansions within template bodies
	const result = `{
		"capabilities": {
			"textDocumentSync": {"openClose": true, "change": 1},
			"hoverProvider": true,
			"referencesProvider": true,
			"definitionProvider": true,
			"semanticTokensProvider": {
				"legend": {"tokenTypes": ["comment", "keyword", "function", "string", "parameter", "variable"], "tokenModifiers": []},
				"full": true
			}
		},
		"serverInfo": {"name": "lblsp"}
	}`
	return s.replyRaw(msg.ID, json.RawMessage(result))
}

func (s *server) handleShutdown(msg *request) error {
	s.shutdown = true
	return s.reply(msg.ID, nil)
}

func (s *server) handleExit() error {
	if s.shutdown {
		return exitError{0}
	}
	return exitError{1}
}

func (s *server) handleDidOpen(msg *request) error {
	var p struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return nil
	}
	doc := newDocument(p.TextDocument.URI, p.TextDocument.Text)
	s.docs[p.TextDocument.URI] = doc
	return s.publishDiagnostics(doc)
}

func (s *server) handleDidChange(msg *request) error {
	var p struct {
		TextDocument   textDocumentIdentifier `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return nil
	}
	doc := s.docs[p.TextDocument.URI]
	if doc == nil || len(p.ContentChanges) == 0 {
		return nil
	}
	doc.setText(p.ContentChanges[len(p.ContentChanges)-1].Text)
	return s.publishDiagnostics(doc)
}

func (s *server) handleDidClose(msg *request) error {
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return nil
	}
	delete(s.docs, p.TextDocument.URI)
	return nil
}

func (s *server) handleHover(msg *request) error {
	if msg.ID == nil {
		return nil
	}
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Position     position               `json:"position"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return s.sendError(msg.ID, codeInvalidParams, err.Error())
	}
	doc := s.docs[p.TextDocument.URI]
	if doc == nil {
		return s.reply(msg.ID, nil)
	}
	name, rng, ok := doc.symbolAt(p.Position.Line, p.Position.Character)
	if !ok {
		return s.reply(msg.ID, nil)
	}
	def := doc.defs[name]
	if def.doc == "" {
		return s.reply(msg.ID, nil)
	}
	return s.reply(msg.ID, struct {
		Contents markupContent `json:"contents"`
		Range    lspRange      `json:"range,omitempty"`
	}{
		Contents: markupContent{Kind: "markdown", Value: def.doc},
		Range:    rng.toLSP(),
	})
}

func (s *server) handleDefinition(msg *request) error {
	if msg.ID == nil {
		return nil
	}
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Position     position               `json:"position"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return s.sendError(msg.ID, codeInvalidParams, err.Error())
	}
	doc := s.docs[p.TextDocument.URI]
	if doc == nil {
		return s.reply(msg.ID, nil)
	}

	// Check if cursor is on an include path
	if includePath, ok := doc.includePathAt(p.Position.Line, p.Position.Character); ok {
		// Include paths are rooted at doc.root with .linebased extension added
		absolutePath := path.Join(doc.root, includePath+".linebased")
		includeURI := "file://" + absolutePath
		return s.reply(msg.ID, location{
			URI:   includeURI,
			Range: span{0, 0, 0, 0}.toLSP(),
		})
	}

	name, _, ok := doc.symbolAt(p.Position.Line, p.Position.Character)
	if !ok {
		return s.reply(msg.ID, nil)
	}
	def, ok := doc.defs[name]
	if !ok {
		return s.reply(msg.ID, nil)
	}
	// Definition location: after "define " on the definition line
	start := utf16Len("define ")
	length := utf16Len(name)
	return s.reply(msg.ID, location{
		URI:   def.uri,
		Range: span{def.line, start, def.line, start + length}.toLSP(),
	})
}

func (s *server) handleReferences(msg *request) error {
	if msg.ID == nil {
		return nil
	}
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Position     position               `json:"position"`
		Context      struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return s.sendError(msg.ID, codeInvalidParams, err.Error())
	}
	doc := s.docs[p.TextDocument.URI]
	if doc == nil {
		return s.reply(msg.ID, nil)
	}
	name, _, ok := doc.symbolAt(p.Position.Line, p.Position.Character)
	if !ok {
		return s.reply(msg.ID, nil)
	}
	refs := doc.references(name, p.Context.IncludeDeclaration)
	locs := make([]location, len(refs))
	for i, ref := range refs {
		locs[i] = location{URI: doc.uri, Range: ref.toLSP()}
	}
	return s.reply(msg.ID, locs)
}

func (s *server) handleSemanticTokens(msg *request) error {
	if msg.ID == nil {
		return nil
	}
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return s.sendError(msg.ID, codeInvalidParams, err.Error())
	}
	var data []uint32
	if doc := s.docs[p.TextDocument.URI]; doc != nil {
		data = doc.semanticTokens()
	}
	if data == nil {
		data = []uint32{}
	}
	return s.reply(msg.ID, struct {
		Data []uint32 `json:"data"`
	}{Data: data})
}

func (s *server) publishDiagnostics(doc *document) error {
	diags := make([]diagnostic, len(doc.errors))
	for i, e := range doc.errors {
		lineLen := 0
		if e.line >= 0 && e.line < len(doc.lines) {
			lineLen = utf16Len(doc.lines[e.line])
		}
		diags[i] = diagnostic{
			Range:    span{e.line, 0, e.line, lineLen}.toLSP(),
			Severity: 1,
			Source:   "lblsp",
			Message:  e.msg,
		}
	}
	return s.notify("textDocument/publishDiagnostics", struct {
		URI         string       `json:"uri"`
		Diagnostics []diagnostic `json:"diagnostics"`
	}{
		URI:         doc.uri,
		Diagnostics: diags,
	})
}

// Protocol I/O

func (s *server) readMessage() ([]byte, error) {
	var contentLen int
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.ToLower(strings.TrimSpace(k)) == "content-length" {
			contentLen, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if contentLen == 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	data := make([]byte, contentLen)
	_, err := io.ReadFull(s.r, data)
	return data, err
}

func (s *server) writeMessage(data []byte) error {
	fmt.Fprintf(s.w, "Content-Length: %d\r\n\r\n", len(data))
	s.w.Write(data)
	return s.w.Flush()
}

func (s *server) reply(id json.RawMessage, result any) error {
	data, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		return err
	}
	return s.writeMessage(data)
}

func (s *server) replyRaw(id json.RawMessage, result json.RawMessage) error {
	data, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		return err
	}
	return s.writeMessage(data)
}

func (s *server) sendError(id json.RawMessage, code int, message string) error {
	data, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Error: &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: code, Message: message},
	})
	if err != nil {
		return err
	}
	return s.writeMessage(data)
}

func (s *server) notify(method string, params any) error {
	data, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return s.writeMessage(data)
}

// LSP Protocol Types

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type diagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
}

// Document

type document struct {
	uri    string
	source string // absolute path to the file
	root   string // root directory for resolving includes
	text   string
	lines  []string
	exprs  []exprInfo
	defs   map[string]definition
	errors []diagError
	fsys   fs.FS // filesystem for resolving includes (nil uses os.DirFS(root))
}

type exprInfo struct {
	expr        linebased.Expression
	line        int    // 0-indexed
	definedName string // for define expressions, the template name
	bodyExprs   []bodyExprInfo // expressions within a define body
}

type bodyExprInfo struct {
	name string // command name
	line int    // 0-indexed document line
}

type definition struct {
	uri    string   // file URI where definition appears
	doc    string
	params []string // parameter names
	line   int      // 0-indexed line of definition
}

type diagError struct {
	line int
	msg  string
}

type span struct{ startLine, startChar, endLine, endChar int }

func (s span) toLSP() lspRange {
	return lspRange{
		Start: position{Line: s.startLine, Character: s.startChar},
		End:   position{Line: s.endLine, Character: s.endChar},
	}
}

func newDocument(uri, text string) *document {
	return newDocumentFS(uri, text, nil)
}

func newDocumentFS(uri, text string, fsys fs.FS) *document {
	source := uri
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		if p, err := url.PathUnescape(u.Path); err == nil && p != "" {
			source = p
		}
	}
	// Root is the directory containing the main file.
	// All include paths are relative to this root.
	root := path.Dir(source)
	d := &document{uri: uri, source: source, root: root, text: text, defs: make(map[string]definition), fsys: fsys}
	d.parse()
	return d
}

func (d *document) setText(text string) {
	d.text = text
	d.parse()
}

func (d *document) parse() {
	d.lines = strings.Split(strings.ReplaceAll(d.text, "\r\n", "\n"), "\n")
	d.exprs = d.exprs[:0]
	d.errors = d.errors[:0]
	clear(d.defs)

	d.parseFile(d.uri, d.source, d.text, nil)

	// Check argument counts and forward references (only for expressions in main file)
	for _, info := range d.exprs {
		if info.expr.Name == "" || info.expr.Name == "define" || info.expr.Name == "include" {
			continue
		}
		def, ok := d.defs[info.expr.Name]
		if !ok {
			continue
		}
		// Only check forward references for definitions in the same file
		if def.uri == d.uri && def.line > info.line {
			d.errors = append(d.errors, diagError{
				line: info.line,
				msg:  fmt.Sprintf("template %q used before definition on line %d", info.expr.Name, def.line+1),
			})
			continue
		}
		numParams := len(def.params)
		numArgs := 0
		for _, a := range info.expr.ParseArgs(numParams + 1) {
			if strings.TrimSpace(a) != "" {
				numArgs++
			}
		}
		if numArgs < numParams {
			d.errors = append(d.errors, diagError{
				line: info.line,
				msg:  fmt.Sprintf("%s requires %d argument(s), got %d", info.expr.Name, numParams, numArgs),
			})
		}
	}
}

// parseFile parses a file and adds its definitions to d.defs.
// seen tracks included files to detect cycles.
func (d *document) parseFile(uri, source, text string, seen map[string]bool) {
	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[source] {
		return // cycle detected, already processed
	}
	seen[source] = true

	dec := linebased.NewDecoder(strings.NewReader(text))
	for {
		expr, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var synErr *linebased.SyntaxError
			if errors.As(err, &synErr) {
				// Only report errors for the main document
				if uri == d.uri {
					d.errors = append(d.errors, diagError{synErr.Line - 1, synErr.Message})
				}
			}
			continue
		}

		// Only track expressions for the main document
		if uri == d.uri {
			info := exprInfo{expr: expr, line: expr.Line - 1}
			if expr.Name == "define" {
				header, bodyText, _ := strings.Cut(expr.Body, "\n")
				fields := strings.Fields(header)
				if len(fields) > 0 {
					info.definedName = fields[0]
				}
				// Parse body expressions for context help
				if bodyText != "" {
					bodyDec := linebased.NewDecoder(strings.NewReader(bodyText))
					for {
						bodyExpr, err := bodyDec.Decode()
						if errors.Is(err, io.EOF) {
							break
						}
						if err != nil {
							break
						}
						if bodyExpr.Name != "" {
							info.bodyExprs = append(info.bodyExprs, bodyExprInfo{
								name: bodyExpr.Name,
								line: info.line + bodyExpr.Line, // document line
							})
						}
					}
				}
			}
			d.exprs = append(d.exprs, info)
		}

		if expr.Name == "define" {
			header, _, _ := strings.Cut(expr.Body, "\n")
			fields := strings.Fields(header)
			if len(fields) > 0 {
				name := fields[0]
				if _, exists := d.defs[name]; !exists {
					d.defs[name] = definition{
						uri:    uri,
						doc:    formatComment(expr.Comment),
						params: fields[1:],
						line:   expr.Line - 1,
					}
				}
			}
		} else if expr.Name == "include" {
			includePath := strings.TrimSpace(strings.Split(expr.Body, "\n")[0])
			if includePath != "" {
				d.processInclude(includePath, seen)
			}
		}
	}
}

// processInclude reads and parses an included file, adding its definitions
// to the document's definition map. Include paths are rooted at the document's
// root directory, matching the behavior of [linebased.ExpandingDecoder].
//
// For example, if /project/main.lb includes "lib", the decoder opens
// "lib.linebased" from the root directory /project/.
func (d *document) processInclude(includePath string, seen map[string]bool) {
	// Include paths are rooted at d.root with .linebased extension added.
	// This matches the behavior of linebased.ExpandingDecoder.
	includePath = includePath + ".linebased"
	absolutePath := path.Join(d.root, includePath)
	includeURI := "file://" + absolutePath

	// Read the included file
	var content []byte
	var err error
	if d.fsys != nil {
		content, err = fs.ReadFile(d.fsys, includePath)
	} else {
		content, err = os.ReadFile(absolutePath)
	}
	if err != nil {
		return // silently ignore missing includes for now
	}

	d.parseFile(includeURI, includePath, string(content), seen)
}

func (d *document) symbolAt(line, char int) (string, span, bool) {
	for _, info := range d.exprs {
		if info.line == line {
			nameLen := utf16Len(info.expr.Name)
			if nameLen > 0 && char < nameLen {
				return info.expr.Name, span{line, 0, line, nameLen}, true
			}
			if info.definedName != "" {
				start := utf16Len(info.expr.Name) + 1
				length := utf16Len(info.definedName)
				if char >= start && char < start+length {
					return info.definedName, span{line, start, line, start + length}, true
				}
			}
		}
		// Check body expressions within defines
		for _, bodyExpr := range info.bodyExprs {
			if bodyExpr.line == line {
				// Body lines have a leading tab, so command starts at char 1
				nameLen := utf16Len(bodyExpr.name)
				if char >= 1 && char < 1+nameLen {
					return bodyExpr.name, span{line, 1, line, 1 + nameLen}, true
				}
			}
		}
	}
	return "", span{}, false
}

// includePathAt returns the include path if cursor is on an include statement's path.
func (d *document) includePathAt(line, char int) (string, bool) {
	for _, info := range d.exprs {
		if info.line != line || info.expr.Name != "include" {
			continue
		}
		// Include path starts after "include "
		start := utf16Len("include ")
		includePath := strings.TrimSpace(strings.Split(info.expr.Body, "\n")[0])
		length := utf16Len(includePath)
		if char >= start && char < start+length {
			return includePath, true
		}
	}
	return "", false
}

func (d *document) references(name string, includeDecl bool) []span {
	var refs []span
	for _, info := range d.exprs {
		if info.expr.Name == name {
			nameLen := utf16Len(name)
			refs = append(refs, span{info.line, 0, info.line, nameLen})
		} else if includeDecl && info.definedName == name {
			start := utf16Len(info.expr.Name) + 1
			length := utf16Len(info.definedName)
			refs = append(refs, span{info.line, start, info.line, start + length})
		}
		// Check body expressions within defines
		for _, bodyExpr := range info.bodyExprs {
			if bodyExpr.name == name {
				nameLen := utf16Len(name)
				// Body lines have a leading tab, so command starts at char 1
				refs = append(refs, span{bodyExpr.line, 1, bodyExpr.line, 1 + nameLen})
			}
		}
	}
	return refs
}

func (d *document) semanticTokens() []uint32 {
	const (
		tokComment   = 0
		tokKeyword   = 1
		tokFunction  = 2
		tokString    = 3 // unused, kept for index stability
		tokParameter = 4
		tokVariable  = 5 // $VAR/${VAR} expansions
	)
	var tokens []semToken

	// Comments
	for i, line := range d.lines {
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		if strings.HasPrefix(trimmed, "#") {
			start := utf16Len(line[:len(line)-len(trimmed)])
			tokens = append(tokens, semToken{i, start, utf16Len(trimmed), tokComment})
		}
	}

	// Commands and definitions
	for _, info := range d.exprs {
		if info.expr.Name == "" {
			continue
		}
		nameLen := utf16Len(info.expr.Name)
		typ := tokFunction
		if info.expr.Name == "define" || info.expr.Name == "include" {
			typ = tokKeyword
		}
		tokens = append(tokens, semToken{info.line, 0, nameLen, typ})

		// For define: emit template name, parameters, and parse body as expressions
		if info.definedName != "" {
			start := nameLen + 1
			tokens = append(tokens, semToken{info.line, start, utf16Len(info.definedName), tokFunction})
			// Parameters after the template name
			if def, ok := d.defs[info.definedName]; ok {
				pos := start + utf16Len(info.definedName)
				header, _, _ := strings.Cut(info.expr.Body, "\n")
				rest := header[len(info.definedName):]
				for _, param := range def.params {
					// Find param in rest
					idx := strings.Index(rest, param)
					if idx >= 0 {
						paramStart := pos + utf16Len(rest[:idx])
						tokens = append(tokens, semToken{info.line, paramStart, utf16Len(param), tokParameter})
					}
				}
			}

			// Parse template body as linebased expressions.
			// The body (after the header line) contains expressions with tabs stripped.
			_, bodyText, _ := strings.Cut(info.expr.Body, "\n")
			if bodyText != "" {
				bodyDec := linebased.NewDecoder(strings.NewReader(bodyText))
				for {
					bodyExpr, err := bodyDec.Decode()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						break
					}
					if bodyExpr.Name == "" {
						continue
					}
					// bodyExpr.Line is 1-indexed within the body.
					// Document line = define line + body expression line.
					docLine := info.line + bodyExpr.Line
					// Body lines have a leading tab in the document, so offset is 1.
					tokens = append(tokens, semToken{docLine, 1, utf16Len(bodyExpr.Name), tokFunction})
					// Scan for variable expansions in the body line
					if docLine < len(d.lines) {
						tokens = append(tokens, scanVariables(docLine, d.lines[docLine], tokVariable)...)
					}
				}
			}
		}
	}

	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		return tokens[i].start < tokens[j].start
	})

	if len(tokens) == 0 {
		return nil
	}
	data := make([]uint32, 0, len(tokens)*5)
	prevLine, prevChar := 0, 0
	for _, t := range tokens {
		deltaLine := t.line - prevLine
		deltaChar := t.start
		if deltaLine == 0 {
			deltaChar = t.start - prevChar
		}
		data = append(data, uint32(deltaLine), uint32(deltaChar), uint32(t.length), uint32(t.typ), 0)
		prevLine, prevChar = t.line, t.start
	}
	return data
}

type semToken struct {
	line, start, length, typ int
}

// scanVariables finds $name and ${name} patterns in a line.
func scanVariables(lineNum int, line string, tokType int) []semToken {
	var tokens []semToken
	i := 0
	for i < len(line) {
		if line[i] != '$' {
			i++
			continue
		}
		start := utf16Len(line[:i])
		i++
		if i >= len(line) {
			break
		}
		if line[i] == '{' {
			// ${name}
			i++
			nameStart := i
			for i < len(line) && line[i] != '}' {
				i++
			}
			if i < len(line) && i > nameStart {
				// Include ${ and }
				length := utf16Len(line[start : i+1])
				tokens = append(tokens, semToken{lineNum, start, length, tokType})
			}
			i++ // skip }
		} else if isIdentStart(line[i]) {
			// $name
			nameStart := i
			for i < len(line) && isIdentContinue(line[i]) {
				i++
			}
			if i > nameStart {
				length := utf16Len(line[start:i])
				tokens = append(tokens, semToken{lineNum, start, length, tokType})
			}
		}
	}
	return tokens
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentContinue(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// Helpers

func formatComment(comment string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(comment, "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		lines = append(lines, line)
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
