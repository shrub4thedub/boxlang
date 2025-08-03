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
	fmt.Printf("DEBUG: parseBlock looking for matching end, starting depth=1 at bodyStart=%d\n", bodyStart)
	blockDepth := 1  // Tracks nested blocks ([main], [fn], etc.)
	controlDepth := 0  // Tracks nested control structures (if, while, for)
	
	for i < len(tokens) && blockDepth > 0 {
		token := tokens[i]
		fmt.Printf("DEBUG: parseBlock at %d: %s = %s, blockDepth=%d, controlDepth=%d\n", 
			i, tokenTypeName(token.Type), token.Value, blockDepth, controlDepth)
		
		if token.Type == boxLexer.Symbols()["BlockStart"] {
			blockDepth++
			fmt.Printf("DEBUG: parseBlock found nested BlockStart, blockDepth now %d\n", blockDepth)
		} else if token.Type == boxLexer.Symbols()["Word"] && 
		          (token.Value == "while" || token.Value == "if" || token.Value == "for") {
			controlDepth++
			fmt.Printf("DEBUG: parseBlock found control structure start, controlDepth now %d\n", controlDepth)
		} else if token.Type == boxLexer.Symbols()["BlockEnd"] {
			if controlDepth > 0 {
				// This 'end' closes a control structure, not a block
				controlDepth--
				fmt.Printf("DEBUG: parseBlock found control structure end, controlDepth now %d\n", controlDepth)
			} else {
				// This 'end' closes a block
				blockDepth--
				fmt.Printf("DEBUG: parseBlock found block end, blockDepth now %d\n", blockDepth)
			}
		}
		
		if blockDepth > 0 {
			i++
		}
	}
	fmt.Printf("DEBUG: parseBlock found matching end at index %d, body range: %d to %d\n", i, bodyStart, i)
	
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
	
	// DEBUG: Print token overview
	fmt.Printf("DEBUG: parseBodyTokens called with %d tokens\n", len(tokens))
	for idx, token := range tokens {
		if token.Type != boxLexer.Symbols()["Whitespace"] && token.Type != boxLexer.Symbols()["Newline"] {
			fmt.Printf("  [%d] %s: %s\n", idx, tokenTypeName(token.Type), token.Value)
		}
	}
	
	for i < len(tokens) {
		if i >= len(tokens) {
			break
		}
		
		fmt.Printf("DEBUG: parseBodyTokens at index %d/%d, token: %s = %s\n", 
			i, len(tokens), tokenTypeName(tokens[i].Type), tokens[i].Value)
		
		// Skip newlines
		if tokens[i].Type == boxLexer.Symbols()["Newline"] {
			fmt.Printf("DEBUG: Skipping newline at %d\n", i)
			i++
			continue
		}
		
		// Check for control structures
		if tokens[i].Type == boxLexer.Symbols()["Word"] && 
		   (tokens[i].Value == "while" || tokens[i].Value == "if" || tokens[i].Value == "for") {
			fmt.Printf("DEBUG: Found control structure %s at index %d\n", tokens[i].Value, i)
			// Parse control structure
			controlBlock, newIndex := p.parseControlStructureTokens(tokens, i)
			fmt.Printf("DEBUG: Control structure parsed, returned index %d (was %d)\n", newIndex, i)
			result = append(result, *controlBlock)
			i = newIndex
		} else {
			fmt.Printf("DEBUG: Parsing regular command at index %d\n", i)
			// Parse regular command
			cmd, newIndex := p.parseCommandFromTokens(tokens, i)
			fmt.Printf("DEBUG: Regular command parsed, returned index %d (was %d)\n", newIndex, i)
			if cmd != nil {
				result = append(result, cmd)
			}
			i = newIndex
		}
	}
	
	fmt.Printf("DEBUG: parseBodyTokens finished with %d results\n", len(result))
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
	
	// Parse arguments
	for _, token := range tokens[1:] {
		expr := p.createExpr(token)
		if expr != nil {
			cmd.Args = append(cmd.Args, expr)
		}
	}
	
	return cmd
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
	
	fmt.Printf("DEBUG: parseControlStructureTokens starting at index %d, token: %s\n", startIndex, startToken.Value)
	
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
				fmt.Printf("DEBUG: Added arg: %s\n", expr.String())
			}
		}
		i++
	}
	
	// Skip newline after control structure header
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["Newline"] {
		fmt.Printf("DEBUG: Skipping newline after control structure header at %d\n", i)
		i++
	}
	
	// Find matching end
	depth := 1
	bodyStart := i
	fmt.Printf("DEBUG: Looking for matching end, bodyStart=%d, depth=%d\n", bodyStart, depth)
	
	for i < len(tokens) && depth > 0 {
		token := tokens[i]
		fmt.Printf("DEBUG: Depth tracking at %d: %s = %s, depth=%d\n", i, tokenTypeName(token.Type), token.Value, depth)
		
		if token.Type == boxLexer.Symbols()["Word"] && 
		   (token.Value == "while" || token.Value == "if" || token.Value == "for") {
			depth++
			fmt.Printf("DEBUG: Found nested control structure, depth now %d\n", depth)
		} else if token.Type == boxLexer.Symbols()["BlockEnd"] {
			depth--
			fmt.Printf("DEBUG: Found BlockEnd, depth now %d\n", depth)
		}
		
		if depth > 0 {
			i++
		}
	}
	
	fmt.Printf("DEBUG: Found matching end at index %d, bodyStart=%d to bodyEnd=%d\n", i, bodyStart, i)
	
	// Parse body
	if i > bodyStart {
		bodyTokens := tokens[bodyStart:i]
		fmt.Printf("DEBUG: Parsing control structure body with %d tokens\n", len(bodyTokens))
		block.Body = p.parseBodyTokens(bodyTokens)
	}
	
	// Skip the 'end' token
	if i < len(tokens) && tokens[i].Type == boxLexer.Symbols()["BlockEnd"] {
		fmt.Printf("DEBUG: Skipping BlockEnd token at %d\n", i)
		i++
	}
	
	fmt.Printf("DEBUG: parseControlStructureTokens returning index %d\n", i)
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

