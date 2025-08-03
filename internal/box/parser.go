package box

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type Expr interface {
	String() string
}

type LiteralExpr struct {
	Value string
}

func (e *LiteralExpr) String() string {
	return e.Value
}

type VariableExpr struct {
	Name  string
	Index *string // nil for $x, non-nil for ${x[*]} or ${x[2]}
}

func (e *VariableExpr) String() string {
	if e.Index == nil {
		return "$" + e.Name
	}
	return "${" + e.Name + "[" + *e.Index + "]}"
}

type HeaderLookupExpr struct {
	Path string
}

func (e *HeaderLookupExpr) String() string {
	return "${" + e.Path + "}"
}

type CommandSubExpr struct {
	Command string
}

func (e *CommandSubExpr) String() string {
	return "`" + e.Command + "`"
}

type ErrorPolicy int

const (
	FailFast ErrorPolicy = iota
	IgnoreError
	FallbackOnError
	TryFallbackHalt
)

type Cmd struct {
	Verb        string
	Args        []Expr
	Redirects   []Redirect
	ErrorPolicy ErrorPolicy
	Fallback    *Cmd
	Line        int
	Column      int
}

type Pipeline struct {
	Commands []Cmd
	Line     int
	Column   int
}

type Redirect struct {
	Type   string // >, >>, 2>
	Target string
}

type BlockType int

const (
	MainBlock BlockType = iota
	FnBlock
	DataBlock
	CustomBlock
)

type BlockModifier struct {
	Flag string // -c, -i, -h
}

type Block struct {
	Type      BlockType
	Label     string
	Args      []string
	Modifiers []BlockModifier
	Body      []interface{} // mix of Cmd and nested Block
	Line      int
	Column    int
}

type Import struct {
	Path      string // Original import path (e.g., "utils/helper.box")
	Namespace string // Derived namespace (e.g., "helper")
	Program   *Program // The imported program
}

type Program struct {
	Blocks     []Block
	Functions  map[string]*Block // Named function blocks
	Data       map[string]*Block // Named data blocks
	Main       *Block            // Main block (if any)
	Imports    []Import          // List of imports
	ImportMap  map[string]*Import // Quick namespace lookup
	Namespaces map[string]map[string]*Block // Namespaced functions/data
}

type Parser struct {
	lexer   *Lexer
	current Token
}

func NewParser(lexer *Lexer) *Parser {
	p := &Parser{lexer: lexer}
	p.advance()
	return p
}

func (p *Parser) advance() {
	p.current = p.lexer.NextToken()
}

func (p *Parser) expect(kind TokenKind) error {
	if p.current.Kind != kind {
		return &BoxError{
			Message:  fmt.Sprintf("expected %v, got %v", kind, p.current.Kind),
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	p.advance()
	return nil
}

func (p *Parser) Parse() (*Program, error) {
	program := &Program{
		Functions:  make(map[string]*Block),
		Data:       make(map[string]*Block),
		ImportMap:  make(map[string]*Import),
		Namespaces: make(map[string]map[string]*Block),
	}
	
	var topLevelCommands []interface{}
	
	for p.current.Kind != EOF {
		if p.current.Kind == COMMENT {
			p.advance()
			continue
		}
		
		if p.current.Kind == HEADER_START {
			block, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			program.Blocks = append(program.Blocks, *block)
			
			// Organize blocks by type
			switch block.Type {
			case FnBlock:
				if block.Label == "" {
					return nil, &BoxError{
						Message:  "function block missing name",
						Location: Location{p.lexer.filename, block.Line, block.Column},
					}
				}
				if _, exists := program.Functions[block.Label]; exists {
					return nil, &BoxError{
						Message:  fmt.Sprintf("function '%s' already defined", block.Label),
						Location: Location{p.lexer.filename, block.Line, block.Column},
					}
				}
				program.Functions[block.Label] = block
			case DataBlock:
				if block.Label == "" {
					return nil, &BoxError{
						Message:  "data block missing name",
						Location: Location{p.lexer.filename, block.Line, block.Column},
					}
				}
				if _, exists := program.Data[block.Label]; exists {
					return nil, &BoxError{
						Message:  fmt.Sprintf("data block '%s' already defined", block.Label),
						Location: Location{p.lexer.filename, block.Line, block.Column},
					}
				}
				program.Data[block.Label] = block
			case MainBlock:
				if program.Main != nil {
					return nil, &BoxError{
						Message:  "multiple [main] blocks not allowed",
						Location: Location{p.lexer.filename, block.Line, block.Column},
					}
				}
				program.Main = block
			}
		} else {
			// Top-level commands (outside any block)
			if p.current.Kind == WORD && p.current.Value == "import" {
				// Handle import statement
				err := p.parseImport(program)
				if err != nil {
					return nil, err
				}
			} else if p.current.Kind == WORD && 
				(p.current.Value == "if" || p.current.Value == "for" || p.current.Value == "while") {
				controlBlock, err := p.parseControlStructure()
				if err != nil {
					return nil, err
				}
				topLevelCommands = append(topLevelCommands, *controlBlock)
			} else {
				cmdOrPipeline, err := p.parseCommandOrPipeline()
				if err != nil {
					return nil, err
				}
				topLevelCommands = append(topLevelCommands, cmdOrPipeline)
			}
		}
	}
	
	// If there are top-level commands but no [main] block, create implicit main
	if len(topLevelCommands) > 0 && program.Main == nil {
		program.Main = &Block{
			Type: MainBlock,
			Body: topLevelCommands,
		}
		program.Blocks = append(program.Blocks, *program.Main)
	}
	
	return program, nil
}

func (p *Parser) parseBlock() (*Block, error) {
	if p.current.Kind != HEADER_START {
		return nil, &BoxError{
			Message:  "expected block header",
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	
	headerContent := p.current.Value[1 : len(p.current.Value)-1] // strip [ ]
	line, column := p.current.Line, p.current.Column
	p.advance()
	
	block := &Block{
		Line:   line,
		Column: column,
		Body:   []interface{}{},
	}
	
	parts := strings.Fields(headerContent)
	if len(parts) == 0 {
		return nil, &BoxError{
			Message:  "empty block header",
			Location: Location{p.lexer.filename, line, column},
		}
	}
	
	// Parse block type first  
	blockTypeStr := parts[0]
	
	// Parse modifiers (they come after block type in Box syntax)
	i := 1
	for i < len(parts) && strings.HasPrefix(parts[i], "-") {
		block.Modifiers = append(block.Modifiers, BlockModifier{Flag: parts[i]})
		i++
	}
	
	// Now i points to the first non-modifier part after the block type
	switch blockTypeStr {
	case "main":
		block.Type = MainBlock
		if i < len(parts) {
			return nil, &BoxError{
				Message:  "[main] block cannot have arguments",
				Location: Location{p.lexer.filename, line, column},
			}
		}
	case "fn":
		block.Type = FnBlock
		if i >= len(parts) {
			return nil, &BoxError{
				Message:  "[fn] block missing function name",
				Location: Location{p.lexer.filename, line, column},
			}
		}
		block.Label = parts[i]
		block.Args = parts[i+1:]
	case "data":
		block.Type = DataBlock
		if i >= len(parts) {
			return nil, &BoxError{
				Message:  "[data] block missing data name",
				Location: Location{p.lexer.filename, line, column},
			}
		}
		block.Label = parts[i]
		// Note: data blocks don't have additional arguments like functions do
	default:
		// Allow custom blocks but validate they're reasonable
		if strings.HasPrefix(blockTypeStr, "-") {
			return nil, &BoxError{
				Message:  fmt.Sprintf("unknown block type '%s' - did you mean 'fn', 'data', or 'main'?", blockTypeStr),
				Location: Location{p.lexer.filename, line, column},
			}
		}
		block.Type = CustomBlock
		block.Label = blockTypeStr
		block.Args = parts[1:]
	}
	
	// Parse block body
	for p.current.Kind != BLOCK_END && p.current.Kind != EOF {
		if p.current.Kind == COMMENT {
			p.advance()
			continue
		}
		
		if p.current.Kind == HEADER_START {
			nestedBlock, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			block.Body = append(block.Body, *nestedBlock)
		} else {
			// Check for control structures
			if p.current.Kind == WORD && 
				(p.current.Value == "if" || p.current.Value == "for" || p.current.Value == "while") {
				controlBlock, err := p.parseControlStructure()
				if err != nil {
					return nil, err
				}
				block.Body = append(block.Body, *controlBlock)
			} else {
				cmdOrPipeline, err := p.parseCommandOrPipeline()
				if err != nil {
					return nil, err
				}
				block.Body = append(block.Body, cmdOrPipeline)
			}
		}
	}
	
	if p.current.Kind == BLOCK_END {
		p.advance()
	}
	
	return block, nil
}

func (p *Parser) parseControlStructure() (*Block, error) {
	if p.current.Kind != WORD {
		return nil, &BoxError{
			Message:  "expected control structure keyword",
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	
	keyword := p.current.Value
	line, column := p.current.Line, p.current.Column
	
	block := &Block{
		Type:   CustomBlock,
		Label:  keyword,
		Line:   line,
		Column: column,
		Body:   []interface{}{},
	}
	
	p.advance() // consume control keyword
	
	// Parse the condition/iteration part on the same line
	for p.current.Kind != EOF && p.current.Kind != COMMENT && 
		p.current.Line == line {
		
		if p.current.Kind == WORD {
			block.Args = append(block.Args, p.current.Value)
			p.advance()
		} else {
			break
		}
	}
	
	// Parse the body until we hit 'end' or 'else'
	for p.current.Kind != BLOCK_END && p.current.Kind != EOF {
		if p.current.Kind == COMMENT {
			p.advance()
			continue
		}
		
		if p.current.Kind == WORD && p.current.Value == "else" {
			// Handle else clause
			p.advance()
			elseBlock := &Block{
				Type:   CustomBlock,
				Label:  "else",
				Line:   p.current.Line,
				Column: p.current.Column,
				Body:   []interface{}{},
			}
			
			// Parse else body
			for p.current.Kind != BLOCK_END && p.current.Kind != EOF {
				if p.current.Kind == COMMENT {
					p.advance()
					continue
				}
				
				if p.current.Kind == HEADER_START {
					nestedBlock, err := p.parseBlock()
					if err != nil {
						return nil, err
					}
					elseBlock.Body = append(elseBlock.Body, *nestedBlock)
				} else if p.current.Kind == WORD && 
					(p.current.Value == "if" || p.current.Value == "for" || p.current.Value == "while") {
					controlBlock, err := p.parseControlStructure()
					if err != nil {
						return nil, err
					}
					elseBlock.Body = append(elseBlock.Body, *controlBlock)
				} else {
					cmd, err := p.parseCommand()
					if err != nil {
						return nil, err
					}
					elseBlock.Body = append(elseBlock.Body, *cmd)
				}
			}
			
			block.Body = append(block.Body, *elseBlock)
			break
		}
		
		if p.current.Kind == HEADER_START {
			nestedBlock, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			block.Body = append(block.Body, *nestedBlock)
		} else if p.current.Kind == WORD && 
			(p.current.Value == "if" || p.current.Value == "for" || p.current.Value == "while") {
			controlBlock, err := p.parseControlStructure()
			if err != nil {
				return nil, err
			}
			block.Body = append(block.Body, *controlBlock)
		} else {
			cmd, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			block.Body = append(block.Body, *cmd)
		}
	}
	
	if p.current.Kind == BLOCK_END {
		p.advance()
	}
	
	return block, nil
}

func (p *Parser) parseCommand() (*Cmd, error) {
	if p.current.Kind != WORD {
		return nil, &BoxError{
			Message:  "expected command verb",
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	
	cmd := &Cmd{
		Verb:        p.current.Value,
		Line:        p.current.Line,
		Column:      p.current.Column,
		ErrorPolicy: FailFast,
	}
	startLine := p.current.Line
	p.advance()
	
	// Parse arguments until we hit something that ends the command
	for p.current.Kind != EOF && p.current.Kind != PIPELINE && 
		p.current.Kind != REDIRECT && p.current.Kind != IGNORE_ERROR &&
		p.current.Kind != HEADER_START && p.current.Kind != BLOCK_END {
		
		if p.current.Kind == COMMENT {
			break
		}
		
		// Stop if we hit '!' (which is used for try-fallback-halt)
		if p.current.Kind == WORD && p.current.Value == "!" {
			break
		}
		
		// Stop if we hit a new line and encounter what looks like a new command
		if p.current.Line > startLine && p.current.Kind == WORD {
			// Check if this word could be a command verb
			if p.isLikelyCommand(p.current.Value) {
				break
			}
		}
		
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		cmd.Args = append(cmd.Args, expr)
	}
	
	// Parse redirections
	for p.current.Kind == REDIRECT {
		redirect := Redirect{Type: p.current.Value}
		p.advance()
		
		if p.current.Kind == WORD {
			redirect.Target = p.current.Value
			p.advance()
		}
		cmd.Redirects = append(cmd.Redirects, redirect)
	}
	
	// Parse error policy and fallback
	if p.current.Kind == IGNORE_ERROR {
		p.advance()
		cmd.ErrorPolicy = IgnoreError
		
		// Check for fallback command after ?
		if p.current.Kind == WORD {
			fallback, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			cmd.Fallback = fallback
			cmd.ErrorPolicy = FallbackOnError
		}
	} else if p.current.Kind == WORD && p.current.Value == "!" {
		// Handle ! fallback (try-fallback-halt)
		p.advance()
		if p.current.Kind == WORD {
			fallback, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			cmd.Fallback = fallback
			cmd.ErrorPolicy = TryFallbackHalt
		} else {
			return nil, &BoxError{
				Message:  "! requires fallback command",
				Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
			}
		}
	}
	
	return cmd, nil
}

// parseCommandOrPipeline parses either a single command or a pipeline of commands
func (p *Parser) parseCommandOrPipeline() (interface{}, error) {
	firstCmd, err := p.parseCommand()
	if err != nil {
		return nil, err
	}
	
	// Check if this is part of a pipeline
	if p.current.Kind == PIPELINE {
		pipeline := &Pipeline{
			Commands: []Cmd{*firstCmd},
			Line:     firstCmd.Line,
			Column:   firstCmd.Column,
		}
		
		// Parse remaining commands in the pipeline
		for p.current.Kind == PIPELINE {
			p.advance() // consume |
			
			nextCmd, err := p.parseCommand()
			if err != nil {
				return nil, err
			}
			pipeline.Commands = append(pipeline.Commands, *nextCmd)
		}
		
		return *pipeline, nil
	}
	
	// Not a pipeline, just return the single command
	return *firstCmd, nil
}

// Helper function to determine if a word is likely a command verb
func (p *Parser) isLikelyCommand(word string) bool {
	// Common Box commands and built-ins
	commands := []string{
		"echo", "set", "run", "cd", "exit", "return", "if", "for", "while",
		"break", "continue", "exists", "test", "copy", "move", "delete",
		"mkdir", "touch", "sleep", "spawn", "wait", "hash", "len", "link",
		"glob", "match", "prompt", "arith", "env",
	}
	
	for _, cmd := range commands {
		if word == cmd {
			return true
		}
	}
	
	// Also check if it looks like a function call (user-defined commands)
	// For now, assume any word that isn't obviously an argument could be a command
	return true
}

func (p *Parser) parseExpression() (Expr, error) {
	switch p.current.Kind {
	case WORD:
		expr := &LiteralExpr{Value: p.current.Value}
		p.advance()
		return expr, nil
		
	case SINGLE_QUOTE:
		expr := &LiteralExpr{Value: p.current.Value}
		p.advance()
		return expr, nil
		
	case DOUBLE_QUOTE:
		// Handle variable expansion in double quotes
		value := p.current.Value
		p.advance()
		
		// For now, create a literal but mark it for expansion
		// TODO: Implement proper interpolation parsing
		expr := &LiteralExpr{Value: value}
		return expr, nil
		
	case VARIABLE:
		name := p.current.Value
		p.advance()
		
		// Parse variable with potential array access
		var index *string
		if strings.Contains(name, "[") {
			// Extract base name and index
			parts := strings.Split(name, "[")
			if len(parts) == 2 && strings.HasSuffix(parts[1], "]") {
				baseName := parts[0]
				indexStr := strings.TrimSuffix(parts[1], "]")
				name = baseName
				index = &indexStr
			}
		}
		
		return &VariableExpr{Name: name, Index: index}, nil
		
	case HEADER_LOOKUP:
		path := p.current.Value
		p.advance()
		return &HeaderLookupExpr{Path: path}, nil
		
	case COMMAND_SUB:
		command := p.current.Value
		p.advance()
		return &CommandSubExpr{Command: command}, nil
		
	default:
		return nil, &BoxError{
			Message:  fmt.Sprintf("unexpected token in expression: %v", p.current.Kind),
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
}

// parseImport handles import statements like "import path/to/file.box"
func (p *Parser) parseImport(program *Program) error {
	if p.current.Kind != WORD || p.current.Value != "import" {
		return &BoxError{
			Message:  "expected 'import'",
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	
	importLine := p.current.Line
	p.advance() // consume 'import'
	
	if p.current.Kind != WORD {
		return &BoxError{
			Message:  "expected import path after 'import'",
			Location: Location{p.lexer.filename, p.current.Line, p.current.Column},
		}
	}
	
	importPath := p.current.Value
	p.advance() // consume path
	
	// Validate .box extension
	if !strings.HasSuffix(importPath, ".box") {
		return &BoxError{
			Message:  "import path must end with .box",
			Location: Location{p.lexer.filename, importLine, p.current.Column},
		}
	}
	
	// Derive namespace from filename
	filename := path.Base(importPath)
	namespace := strings.TrimSuffix(filename, ".box")
	
	// Check for namespace collision
	if _, exists := program.ImportMap[namespace]; exists {
		return &BoxError{
			Message:  fmt.Sprintf("namespace '%s' already imported", namespace),
			Location: Location{p.lexer.filename, importLine, p.current.Column},
		}
	}
	
	// Check for collision with local functions/data
	if _, exists := program.Functions[namespace]; exists {
		return &BoxError{
			Message:  fmt.Sprintf("namespace '%s' conflicts with local function", namespace),
			Location: Location{p.lexer.filename, importLine, p.current.Column},
		}
	}
	if _, exists := program.Data[namespace]; exists {
		return &BoxError{
			Message:  fmt.Sprintf("namespace '%s' conflicts with local data block", namespace),
			Location: Location{p.lexer.filename, importLine, p.current.Column},
		}
	}
	
	// Load and parse the imported file
	importedProgram, err := p.loadImportedFile(importPath)
	if err != nil {
		return &BoxError{
			Message:  fmt.Sprintf("failed to import '%s': %v", importPath, err),
			Location: Location{p.lexer.filename, importLine, p.current.Column},
		}
	}
	
	// Create import record
	importRecord := Import{
		Path:      importPath,
		Namespace: namespace,
		Program:   importedProgram,
	}
	
	// Add to program
	program.Imports = append(program.Imports, importRecord)
	program.ImportMap[namespace] = &importRecord
	
	// Create namespace maps for functions and data
	program.Namespaces[namespace] = make(map[string]*Block)
	
	// Add imported functions and data to namespace (ignore main block)
	for name, fn := range importedProgram.Functions {
		program.Namespaces[namespace][name] = fn
	}
	for name, data := range importedProgram.Data {
		program.Namespaces[namespace][name] = data
	}
	
	return nil
}

// loadImportedFile loads and parses a .box file for importing
func (p *Parser) loadImportedFile(importPath string) (*Program, error) {
	// Resolve relative paths relative to current file's directory
	var resolvedPath string
	if filepath.IsAbs(importPath) {
		resolvedPath = importPath
	} else {
		// Get directory of current file being parsed
		currentDir := filepath.Dir(p.lexer.filename)
		resolvedPath = filepath.Join(currentDir, importPath)
	}
	
	// Load the file
	lexer, err := NewLexerFromFile(resolvedPath)
	if err != nil {
		return nil, err
	}
	
	// Parse the imported file
	parser := NewParser(lexer)
	program, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	
	return program, nil
}