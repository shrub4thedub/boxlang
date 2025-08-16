package box

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// Types needed for compatibility with existing code
type ErrorPolicy int

const (
	FailFast ErrorPolicy = iota
	IgnoreError
	FallbackOnError
	TryFallbackHalt
)

type BlockType int

const (
	MainBlock BlockType = iota
	FuncBlock
	DataBlock
	CustomBlock
)

type BlockModifier struct {
	Flag string // -c, -i, -h
}

type Import struct {
	Path      string   // Original import path (e.g., "utils/helper.box")
	Namespace string   // Derived namespace (e.g., "helper")
	Program   *Program // The imported program
}

// Expr interface for compatibility
type Expr interface {
	String() string
}

// LiteralExpr for compatibility
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

type BlockLookupExpr struct {
	Path string
}

func (e *BlockLookupExpr) String() string {
	return "${" + e.Path + "}"
}

type CommandSubExpr struct {
	Command string
}

func (e *CommandSubExpr) String() string {
	return "`" + e.Command + "`"
}

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

type Block struct {
	Type      BlockType
	Label     string
	Args      []string
	Modifiers []BlockModifier
	Body      []interface{} // mix of Cmd and nested Block
	Line      int
	Column    int
}

type Program struct {
	Blocks     []Block
	Functions  map[string]*Block            // Named function blocks
	Data       map[string]*Block            // Named data blocks
	Main       *Block                       // Main block (if any)
	Imports    []Import                     // List of imports
	ImportMap  map[string]*Import           // Quick namespace lookup
	Namespaces map[string]map[string]*Block // Namespaced functions/data
}

// Lexer definition for Box language
var boxLexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		{`Comment`, `#[^\n]*`, nil},
		{`Newline`, `\n`, nil},
		{`Whitespace`, `[ \t\r]+`, nil},
		{`BlockStart`, `\[[^\]]+\]`, nil},
		{`BlockEnd`, `\bend\b`, nil},
		{`CommandSub`, "`[^`]*`", nil},
		{`CommandSubDollar`, `\$\([^)]*\)`, nil},
		{`BlockLookup`, `\$[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_.]*`, nil},
		{`Variable`, `\$\{[^}]+\}|\$[a-zA-Z_][a-zA-Z0-9_]*(?:\[[^\]]*\])?|\$[0-9]+`, nil},
		{`DoubleQuote`, `"(?:[^"\\]|\\.)*"`, nil},
		{`SingleQuote`, `'[^']*'`, nil},
		{`Pipeline`, `\|`, nil},
		{`IgnoreError`, `\?`, nil},
		{`Word`, `[^\s|>?#'"$\[\]`+"`"+`]+`, nil},
	},
})

// Simple AST for Participle parsing
type SimpleProgram struct {
	Elements []Element `Newline* ( @@ ( Newline+ @@ )* )? Newline*`
}

type Element struct {
	Cmd    *SimpleCmd   `@@`
	Block  *SimpleBlock `| @@`
	Import *ImportStmt  `| @@`
}

type ImportStmt struct {
	_    string `"import"`
	Path string `@Word`
}

type SimpleBlock struct {
	Header string `@BlockStart`
	// Don't parse body with Participle - handle manually
}

type SimpleElement struct {
	Cmd *SimpleCmd `@@`
}

type SimpleCmd struct {
	Verb     string       `@Word`
	Args     []SimpleExpr `@@*`
	Pipeline *SimpleCmd   `( "|" @@ )?`
}


type SimpleExpr struct {
	Value string `@DoubleQuote | @SingleQuote | @CommandSub | @CommandSubDollar | @Variable | @BlockLookup | @Word`
	Type  string // We'll determine the type from the token during conversion
}

// Parser implementation
type ParticleParser struct {
	filename string
}

// NewParticleParser creates a new parser using Participle
func NewParticleParser(filename string) (*ParticleParser, error) {
	return &ParticleParser{
		filename: filename,
	}, nil
}

// ParseFile parses a Box file
func (p *ParticleParser) ParseFile(filename string) (*Program, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return p.ParseString(string(content))
}

// ParseString parses Box source code from a string
func (p *ParticleParser) ParseString(source string) (*Program, error) {
	// Use manual parsing for better block handling
	return p.parseManually(source)
}

// parseManually handles the parsing manually using the lexer tokens
func (p *ParticleParser) parseManually(source string) (*Program, error) {
	// Create lexer
	lex, err := boxLexer.LexString(p.filename, source)
	if err != nil {
		return nil, &BoxError{
			Message:  fmt.Sprintf("lexer error: %v", err),
			Location: Location{p.filename, 0, 0},
		}
	}

	// Parse tokens into program structure  
	return p.parseTokens(lex)
}

// parseTokens manually parses lexer tokens into a Program
func (p *ParticleParser) parseTokens(lex lexer.Lexer) (*Program, error) {
	program := &Program{
		Functions:  make(map[string]*Block),
		Data:       make(map[string]*Block),
		ImportMap:  make(map[string]*Import),
		Namespaces: make(map[string]map[string]*Block),
	}

	// Collect all tokens first
	var tokens []lexer.Token
	for {
		token, err := lex.Next()
		if err != nil {
			return nil, err
		}
		if token.EOF() {
			break
		}
		// Skip comments and whitespace
		if token.Type == boxLexer.Symbols()["Comment"] || token.Type == boxLexer.Symbols()["Whitespace"] {
			continue
		}
		tokens = append(tokens, token)
	}

	// Parse the token stream
	var topLevelCommands []interface{}
	i := 0
	
	for i < len(tokens) {
		token := tokens[i]
		
		// Skip newlines at top level
		if token.Type == boxLexer.Symbols()["Newline"] {
			i++
			continue
		}
		
		// Handle import statements
		if token.Type == boxLexer.Symbols()["Word"] && token.Value == "import" {
			importStmt, newIndex, err := p.parseImport(tokens, i)
			if err != nil {
				return nil, err
			}
			
			// Process the import
			err = p.processImport(program, importStmt)
			if err != nil {
				return nil, err
			}
			
			i = newIndex
			continue
		}
		
		// Handle blocks
		if token.Type == boxLexer.Symbols()["BlockStart"] {
			block, newIndex, err := p.parseBlock(tokens, i)
			if err != nil {
				return nil, err
			}
			
			program.Blocks = append(program.Blocks, *block)
			
			// Organize blocks by type
			switch block.Type {
			case FuncBlock:
				program.Functions[block.Label] = block
			case DataBlock:
				program.Data[block.Label] = block
			case MainBlock:
				program.Main = block
			}
			
			i = newIndex
		} else {
			// Handle top-level commands
			cmd, newIndex, err := p.parseCommand(tokens, i)
			if err != nil {
				return nil, err
			}
			topLevelCommands = append(topLevelCommands, cmd)
			i = newIndex
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

// parseImport parses an import statement from tokens
func (p *ParticleParser) parseImport(tokens []lexer.Token, startIndex int) (*ImportStmt, int, error) {
	if startIndex >= len(tokens) {
		return nil, startIndex, &BoxError{
			Message: "unexpected end of input in import",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Expect "import" token
	if tokens[startIndex].Value != "import" {
		return nil, startIndex, &BoxError{
			Message: "expected 'import'",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Expect path token
	if startIndex+1 >= len(tokens) {
		return nil, startIndex, &BoxError{
			Message: "expected import path after 'import'",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	pathToken := tokens[startIndex+1]
	if pathToken.Type != boxLexer.Symbols()["Word"] {
		return nil, startIndex, &BoxError{
			Message: "expected import path",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	importStmt := &ImportStmt{
		Path: pathToken.Value,
	}
	
	// Skip to next statement (consume newline if present)
	i := startIndex + 2
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		i++
	}
	
	return importStmt, i, nil
}

// processImport processes an import statement by loading the imported file
func (p *ParticleParser) processImport(program *Program, importStmt *ImportStmt) error {
	// Derive namespace from path (e.g., "utils/helper" -> "helper", "tui" -> "tui")
	namespace := importStmt.Path
	if strings.Contains(namespace, "/") {
		parts := strings.Split(namespace, "/")
		namespace = parts[len(parts)-1]
	}
	
	// Construct full file path - try both with and without .box extension
	var filePath string
	var content []byte
	var err error
	
	if strings.HasSuffix(importStmt.Path, ".box") {
		// Use path as-is if it already has .box extension
		filePath = importStmt.Path
		content, err = os.ReadFile(filePath)
	} else {
		// Try without .box extension first
		filePath = importStmt.Path
		content, err = os.ReadFile(filePath)
		
		// If that fails, try with .box extension
		if err != nil {
			filePath = importStmt.Path + ".box"
			content, err = os.ReadFile(filePath)
		}
	}
	
	if err != nil {
		return &BoxError{
			Message: fmt.Sprintf("failed to read import file '%s': %v", filePath, err),
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Parse the imported file
	importParser, err := NewParticleParser(filePath)
	if err != nil {
		return &BoxError{
			Message: fmt.Sprintf("failed to create parser for import '%s': %v", filePath, err),
			Location: Location{p.filename, 0, 0},
		}
	}
	
	importedProgram, err := importParser.ParseString(string(content))
	if err != nil {
		return &BoxError{
			Message: fmt.Sprintf("failed to parse import '%s': %v", filePath, err),
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Create Import object
	importObj := Import{
		Path:      importStmt.Path,
		Namespace: namespace,
		Program:   importedProgram,
	}
	
	// Add to program
	program.Imports = append(program.Imports, importObj)
	program.ImportMap[namespace] = &importObj
	
	// Add imported functions and data to namespaces
	if program.Namespaces[namespace] == nil {
		program.Namespaces[namespace] = make(map[string]*Block)
	}
	
	// Copy functions from imported program to namespace
	for name, block := range importedProgram.Functions {
		program.Namespaces[namespace][name] = block
	}
	
	// Copy data blocks from imported program to namespace
	for name, block := range importedProgram.Data {
		program.Namespaces[namespace][name] = block
	}
	
	return nil
}

// parseBlock parses a block starting from a BlockStart token
func (p *ParticleParser) parseBlock(tokens []lexer.Token, startIndex int) (*Block, int, error) {
	blockToken := tokens[startIndex]
	
	block := &Block{
		Body: []interface{}{},
		Line: blockToken.Pos.Line,
		Column: blockToken.Pos.Column,
	}
	
	// Parse block header
	blockContent := strings.TrimSpace(blockToken.Value)
	if len(blockContent) >= 2 && blockContent[0] == '[' && blockContent[len(blockContent)-1] == ']' {
		blockContent = blockContent[1 : len(blockContent)-1]
	}
	
	err := p.parseBlockHeader(block, blockContent)
	if err != nil {
		return nil, startIndex, err
	}
	
	// Find the matching end token
	i := startIndex + 1
	bodyStart := i
	
	// Skip newlines after header
	for i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		i++
	}
	bodyStart = i
	
	// Find matching end - need to track both block depth and control structure depth
	blockDepth := 1  // Tracks nested blocks ([main], [fn], etc.)
	controlDepth := 0  // Tracks nested control structures (if, while, for)
	
	for i < len(tokens) && blockDepth > 0 {
		token := tokens[i]
		
		if token.Type == boxLexer.Symbols()["BlockStart"] {
			blockDepth++
		} else if token.Type == boxLexer.Symbols()["Word"] && 
		          (token.Value == "while" || token.Value == "if" || token.Value == "for") &&
		          (block.Type == FuncBlock || block.Type == MainBlock) {
			// Only track control structures in function/main blocks, not data blocks
			controlDepth++
		} else if token.Type == boxLexer.Symbols()["BlockEnd"] {
			if controlDepth > 0 {
				// This 'end' closes a control structure, not a block
				controlDepth--
			} else {
				// This 'end' closes a block
				blockDepth--
			}
		}
		
		if blockDepth > 0 {
			i++
		}
	}
	
	if blockDepth > 0 {
		return nil, startIndex, &BoxError{
			Message: "unclosed block",
			Location: Location{p.filename, blockToken.Pos.Line, blockToken.Pos.Column},
		}
	}
	
	// Parse body tokens
	if i > bodyStart {
		bodyTokens := tokens[bodyStart:i]
		block.Body = p.parseBodyTokens(bodyTokens)
	}
	
	return block, i + 1, nil // i points to 'end', return i+1 to skip it
}

// parseCommand parses a single command starting from the given index
func (p *ParticleParser) parseCommand(tokens []lexer.Token, startIndex int) (interface{}, int, error) {
	if startIndex >= len(tokens) {
		return nil, startIndex, &BoxError{
			Message: "unexpected end of input",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Collect tokens until newline or end of tokens
	var cmdTokens []lexer.Token
	i := startIndex
	
	for i < len(tokens) && tokens[i].Type != boxLexer.Symbols()["Newline"] {
		cmdTokens = append(cmdTokens, tokens[i])
		i++
	}
	
	if len(cmdTokens) == 0 {
		return nil, i, &BoxError{
			Message: "empty command",
			Location: Location{p.filename, 0, 0},
		}
	}
	
	// Parse the command tokens
	cmd := p.parseCommandTokens(cmdTokens)
	
	// Skip the newline
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		i++
	}
	
	return cmd, i, nil
}

// parseBodyTokens parses tokens within a block body
func (p *ParticleParser) parseBodyTokens(tokens []lexer.Token) []interface{} {
	var result []interface{}
	i := 0
	
	// DEBUG: Print token overview (commented out)
	// fmt.Printf("DEBUG: parseBodyTokens called with %d tokens\n", len(tokens))
	
	for i < len(tokens) {
		if i >= len(tokens) {
			break
		}
		
		// Skip newlines
		if tokens[i].Type == boxLexer.Symbols()["Newline"] {
			i++
			continue
		}
		
		// Check for control structures
		if tokens[i].Type == boxLexer.Symbols()["Word"] && 
		   (tokens[i].Value == "while" || tokens[i].Value == "if" || tokens[i].Value == "for" || 
		    tokens[i].Value == "elif" || tokens[i].Value == "else") {
			// Parse control structure
			controlBlock, newIndex := p.parseControlStructureTokens(tokens, i)
			result = append(result, *controlBlock)
			i = newIndex
		} else {
			// Parse regular command
			cmd, newIndex := p.parseCommandFromTokens(tokens, i)
			if cmd != nil {
				result = append(result, cmd)
			}
			i = newIndex
		}
	}
	
	return result
}

// Helper function to get token type name for debugging
func tokenTypeName(tokenType lexer.TokenType) string {
	for name, t := range boxLexer.Symbols() {
		if t == tokenType {
			return name
		}
	}
	return "UNKNOWN"
}

// parseCommandTokens creates a Cmd from a sequence of tokens
func (p *ParticleParser) parseCommandTokens(tokens []lexer.Token) interface{} {
	if len(tokens) == 0 {
		return nil
	}
	
	// Find pipeline separators
	var commands [][]lexer.Token
	var currentCmd []lexer.Token
	
	for _, token := range tokens {
		if token.Type == boxLexer.Symbols()["Pipeline"] {
			if len(currentCmd) > 0 {
				commands = append(commands, currentCmd)
				currentCmd = []lexer.Token{}
			}
		} else {
			currentCmd = append(currentCmd, token)
		}
	}
	if len(currentCmd) > 0 {
		commands = append(commands, currentCmd)
	}
	
	if len(commands) == 1 {
		// Single command
		cmd := p.createCmd(commands[0])
		if cmd == nil {
			return nil
		}
		return *cmd
	} else {
		// Pipeline
		pipeline := &Pipeline{
			Commands: []Cmd{},
		}
		for _, cmdTokens := range commands {
			cmd := p.createCmd(cmdTokens)
			if cmd != nil {
				pipeline.Commands = append(pipeline.Commands, *cmd)
			}
		}
		return *pipeline
	}
}

// createCmd creates a Cmd from tokens
func (p *ParticleParser) createCmd(tokens []lexer.Token) *Cmd {
	if len(tokens) == 0 {
		return nil
	}
	
	cmd := &Cmd{
		Verb:        tokens[0].Value,
		Args:        []Expr{},
		Redirects:   []Redirect{},
		ErrorPolicy: FailFast,
		Line:        tokens[0].Pos.Line,
		Column:      tokens[0].Pos.Column,
	}
	
	// Parse arguments - group consecutive tokens that should be concatenated
	argTokens := tokens[1:]
	i := 0
	for i < len(argTokens) {
		// Collect consecutive non-whitespace tokens into a single argument
		var argGroup []lexer.Token
		
		// Add the current token
		argGroup = append(argGroup, argTokens[i])
		i++
		
		// Check if the next tokens are adjacent (no space between them in source)
		for i < len(argTokens) {
			currentToken := argTokens[i-1]
			nextToken := argTokens[i]
			
			// If tokens are adjacent in the source (no space between them),
			// they should be part of the same argument
			if currentToken.Pos.Line == nextToken.Pos.Line &&
				currentToken.Pos.Column+len(currentToken.Value) == nextToken.Pos.Column {
				argGroup = append(argGroup, nextToken)
				i++
			} else {
				break
			}
		}
		
		// Create a single expression from the grouped tokens
		if len(argGroup) == 1 {
			// Single token - use existing logic
			expr := p.createExpr(argGroup[0])
			if expr != nil {
				cmd.Args = append(cmd.Args, expr)
			}
		} else {
			// Multiple adjacent tokens - create a compound expression
			expr := p.createCompoundExpr(argGroup)
			if expr != nil {
				cmd.Args = append(cmd.Args, expr)
			}
		}
	}
	
	return cmd
}

// createCompoundExpr creates a single expression from multiple adjacent tokens
func (p *ParticleParser) createCompoundExpr(tokens []lexer.Token) Expr {
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) == 1 {
		return p.createExpr(tokens[0])
	}
	
	// Combine all tokens into a single literal expression that will be
	// expanded during evaluation. This preserves the variable expansion
	// behavior while treating adjacent tokens as one argument.
	var combined strings.Builder
	for _, token := range tokens {
		combined.WriteString(token.Value)
	}
	
	return &LiteralExpr{Value: combined.String()}
}

// createExpr creates an Expr from a token
func (p *ParticleParser) createExpr(token lexer.Token) Expr {
	value := token.Value
	
	// Determine the type based on the token type
	switch token.Type {
	case boxLexer.Symbols()["DoubleQuote"]:
		// Remove quotes and process escape sequences
		if len(value) >= 2 {
			value = value[1 : len(value)-1]
		}
		value = p.processEscapeSequences(value)
		return &LiteralExpr{Value: value}
		
	case boxLexer.Symbols()["SingleQuote"]:
		// Remove quotes
		if len(value) >= 2 {
			value = value[1 : len(value)-1]
		}
		return &LiteralExpr{Value: value}
		
	case boxLexer.Symbols()["Variable"]:
		return p.parseVariable(value)
		
	case boxLexer.Symbols()["BlockLookup"]:
		path := value
		if strings.HasPrefix(path, "$") {
			path = path[1:]
		}
		return &BlockLookupExpr{Path: path}
		
	case boxLexer.Symbols()["CommandSub"], boxLexer.Symbols()["CommandSubDollar"]:
		command := value
		if strings.HasPrefix(command, "`") && strings.HasSuffix(command, "`") {
			command = command[1 : len(command)-1]
		} else if strings.HasPrefix(command, "$(") && strings.HasSuffix(command, ")") {
			command = command[2 : len(command)-1]
		}
		return &CommandSubExpr{Command: command}
		
	default:
		return &LiteralExpr{Value: value}
	}
}

// parseVariable parses a variable token into a VariableExpr
func (p *ParticleParser) parseVariable(value string) Expr {
	name := value
	if strings.HasPrefix(name, "$") {
		name = name[1:]
	}
	// Remove {} wrapper if present
	if strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
		name = name[1 : len(name)-1]
	}
	
	// Handle array access
	var index *string
	if strings.Contains(name, "[") {
		parts := strings.Split(name, "[")
		if len(parts) == 2 && strings.HasSuffix(parts[1], "]") {
			baseName := parts[0]
			indexStr := strings.TrimSuffix(parts[1], "]")
			name = baseName
			index = &indexStr
		}
	}
	return &VariableExpr{Name: name, Index: index}
}

// parseCommandFromTokens parses a command from a token slice, finding the end
func (p *ParticleParser) parseCommandFromTokens(tokens []lexer.Token, startIndex int) (interface{}, int) {
	if startIndex >= len(tokens) {
		return nil, startIndex
	}
	
	// Collect tokens until newline
	var cmdTokens []lexer.Token
	i := startIndex
	
	for i < len(tokens) && tokens[i].Type != boxLexer.Symbols()["Newline"] {
		cmdTokens = append(cmdTokens, tokens[i])
		i++
	}
	
	// Skip the newline
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		i++
	}
	
	return p.parseCommandTokens(cmdTokens), i
}

// parseControlStructureTokens parses control structures manually
func (p *ParticleParser) parseControlStructureTokens(tokens []lexer.Token, startIndex int) (*Block, int) {
	startToken := tokens[startIndex]
	
	block := &Block{
		Type:  CustomBlock,
		Label: startToken.Value,
		Args:  []string{},
		Body:  []interface{}{},
		Line:  startToken.Pos.Line,
		Column: startToken.Pos.Column,
	}
	
	// Parse arguments until newline
	i := startIndex + 1
	for i < len(tokens) && tokens[i].Type != boxLexer.Symbols()["Newline"] {
		if tokens[i].Type != boxLexer.Symbols()["Whitespace"] {
			expr := p.createExpr(tokens[i])
			if expr != nil {
				block.Args = append(block.Args, expr.String())
			}
		}
		i++
	}
	
	// Skip newline after control structure header
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		i++
	}
	
	// Find matching end
	depth := 1
	bodyStart := i
	
	for i < len(tokens) && depth > 0 {
		token := tokens[i]
		
		if token.Type == boxLexer.Symbols()["Word"] && 
		   (token.Value == "while" || token.Value == "if" || token.Value == "for") {
			depth++
		} else if token.Type == boxLexer.Symbols()["BlockEnd"] {
			depth--
		}
		
		if depth > 0 {
			i++
		}
	}
	
	// Parse body
	if i > bodyStart {
		bodyTokens := tokens[bodyStart:i]
		block.Body = p.parseBodyTokens(bodyTokens)
	}
	
	// Skip the 'end' token
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["BlockEnd"] {
		i++
	}
	
	return block, i
}

// parseBlockHeader parses the block header content
func (p *ParticleParser) parseBlockHeader(block *Block, blockContent string) error {
	parts := strings.Fields(blockContent)
	if len(parts) == 0 {
		return &BoxError{
			Message:  "empty block header",
			Location: Location{p.filename, 0, 0},
		}
	}

	blockTypeStr := parts[0]
	i := 1

	// Parse modifiers
	for i < len(parts) && strings.HasPrefix(parts[i], "-") {
		block.Modifiers = append(block.Modifiers, BlockModifier{Flag: parts[i]})
		i++
	}

	// Set block type and parse arguments
	switch blockTypeStr {
	case "main":
		block.Type = MainBlock
		if i < len(parts) {
			return &BoxError{
				Message:  "[main] block cannot have arguments",
				Location: Location{p.filename, 0, 0},
			}
		}
	case "fn":
		block.Type = FuncBlock
		if i >= len(parts) {
			return &BoxError{
				Message:  "[fn] block missing function name",
				Location: Location{p.filename, 0, 0},
			}
		}
		block.Label = parts[i]
		block.Args = parts[i+1:]
	case "data":
		block.Type = DataBlock
		if i >= len(parts) {
			return &BoxError{
				Message:  "[data] block missing data name",
				Location: Location{p.filename, 0, 0},
			}
		}
		block.Label = parts[i]
	default:
		block.Type = CustomBlock
		block.Label = blockTypeStr
		block.Args = parts[1:]
	}

	return nil
}

// processEscapeSequences processes escape sequences in double-quoted strings
func (p *ParticleParser) processEscapeSequences(value string) string {
	result := ""
	i := 0
	for i < len(value) {
		if value[i] == '\\' && i+1 < len(value) {
			switch value[i+1] {
			case 'n':
				result += "\n"
			case 't':
				result += "\t"
			case 'r':
				result += "\r"
			case '\\':
				result += "\\"
			case '"':
				result += "\""
			default:
				// Unknown escape sequence, keep as-is
				result += string(value[i]) + string(value[i+1])
			}
			i += 2
		} else {
			result += string(value[i])
			i++
		}
	}
	return result
}

// DebugToken represents a token for debugging
type DebugToken struct {
	Type   string
	Value  string
	Line   int
	Column int
	EOF    bool
}

// DebugLexer wraps the Participle lexer for debugging
type DebugLexer struct {
	lexer   lexer.Lexer
	symbols map[string]lexer.TokenType
}

// NewLexerForDebug creates a lexer for debugging token output
func NewLexerForDebug(content, filename string) (*DebugLexer, error) {
	lex, err := boxLexer.LexString(filename, content)
	if err != nil {
		return nil, err
	}
	
	return &DebugLexer{
		lexer:   lex,
		symbols: boxLexer.Symbols(),
	}, nil
}

// NextToken returns the next token for debugging
func (d *DebugLexer) NextToken() (*DebugToken, error) {
	token, err := d.lexer.Next()
	if err != nil {
		return nil, err
	}
	
	if token.EOF() {
		return &DebugToken{
			Type:   "EOF",
			Value:  "",
			Line:   token.Pos.Line,
			Column: token.Pos.Column,
			EOF:    true,
		}, nil
	}
	
	// Find token type name
	typeName := "UNKNOWN"
	for name, t := range d.symbols {
		if t == token.Type {
			typeName = name
			break
		}
	}
	
	return &DebugToken{
		Type:   typeName,
		Value:  token.Value,
		Line:   token.Pos.Line,
		Column: token.Pos.Column,
		EOF:    false,
	}, nil
}

