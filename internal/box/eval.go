package box

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Value []string

func (v Value) String() string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (v Value) List() []string {
	return []string(v)
}

type Scope struct {
	Variables         map[string]Value
	Functions         map[string]*Block
	Data              map[string]map[string]Value
	Namespaces        map[string]map[string]*Block // Imported namespaces
	CurrentNamespace  string                       // Current namespace context for function calls
	Parent            *Scope
}

func NewScope() *Scope {
	return &Scope{
		Variables:  make(map[string]Value),
		Functions:  make(map[string]*Block),
		Data:       make(map[string]map[string]Value),
		Namespaces: make(map[string]map[string]*Block),
	}
}

func (s *Scope) Get(name string) (Value, bool) {
	if val, ok := s.Variables[name]; ok {
		return val, true
	}
	if s.Parent != nil {
		return s.Parent.Get(name)
	}
	return nil, false
}

func (s *Scope) Set(name string, value Value) {
	s.Variables[name] = value
}

func (s *Scope) Child() *Scope {
	return &Scope{
		Variables:  make(map[string]Value),
		Functions:  make(map[string]*Block),
		Data:       make(map[string]map[string]Value),
		Namespaces: make(map[string]map[string]*Block),
		Parent:     s,
	}
}

type HaltType int

const (
	NoHalt HaltType = iota
	BreakHalt
	ContinueHalt  
	ReturnHalt
	ExitHalt
)

type Result struct {
	Status   int
	Halt     bool      // Kept for backward compatibility
	HaltType HaltType  // More specific halt reason
	Error    error
}

type Evaluator struct {
	scope    *Scope
	builtins map[string]BuiltinFunc
	filename string
}

func NewEvaluator(scope *Scope) *Evaluator {
	e := &Evaluator{
		scope:    scope,
		builtins: builtins,
	}
	return e
}

func NewEvaluatorWithFilename(scope *Scope, filename string) *Evaluator {
	e := &Evaluator{
		scope:    scope,
		builtins: builtins,
		filename: filename,
	}
	return e
}

func (e *Evaluator) Eval(program *Program, args []string) Result {
	// Set command line arguments
	e.scope.Set("argv", Value(args))
	if len(args) > 0 {
		e.scope.Set("0", Value{args[0]})
	}
	for i, arg := range args {
		e.scope.Set(strconv.Itoa(i+1), Value{arg})
	}

	// Initialize status variable
	e.scope.Set("status", Value{"0"})

	// Populate namespaces from imports
	for namespace, blocks := range program.Namespaces {
		e.scope.Namespaces[namespace] = blocks
	}

	// Load imported data blocks into namespaced scope
	for _, imp := range program.Imports {
		// Create evaluator for imported program to load its data
		importScope := NewScope()
		importEvaluator := NewEvaluator(importScope)

		// Load data blocks from imported program
		for i, block := range imp.Program.Blocks {
			if block.Type == DataBlock {
				result := importEvaluator.loadDataBlock(&imp.Program.Blocks[i])
				if result.Error != nil {
					return result
				}
			}
		}

		// Copy loaded data to main scope with namespace prefix
		for blockName, dataMap := range importScope.Data {
			namespacedName := imp.Namespace + "." + blockName
			e.scope.Data[namespacedName] = dataMap
		}
	}

	// First pass: collect functions and data blocks
	for i, block := range program.Blocks {
		if block.Type == FuncBlock {
			e.scope.Functions[block.Label] = &program.Blocks[i] // Use address from slice
		} else if block.Type == DataBlock {
			result := e.loadDataBlock(&block)
			if result.Error != nil {
				return result
			}
		}
	}

	// Check for CLI dispatch to -i functions
	if len(args) > 0 {
		if fn, exists := program.Functions[args[0]]; exists {
			for _, mod := range fn.Modifiers {
				if mod.Flag == "-i" {
					return e.callFunction(fn, args[1:])
				}
			}
		}
	}

	// Execute main block if it exists
	if program.Main != nil {
		result := e.evalBlock(program.Main)
		if result.Error != nil || result.Halt {
			return result
		}
	}

	return Result{Status: 0}
}

func (e *Evaluator) loadDataBlock(block *Block) Result {
	if e.scope.Data[block.Label] == nil {
		e.scope.Data[block.Label] = make(map[string]Value)
	}

	// Parse data block body into key-value pairs
	for _, item := range block.Body {
		if cmd, ok := item.(Cmd); ok {
			// In data blocks, commands are treated as key-value assignments
			if len(cmd.Args) > 0 {
				key := cmd.Verb
				var values []string
				for _, arg := range cmd.Args {
					val, err := e.evalExpression(arg)
					if err != nil {
						return Result{Error: err}
					}
					values = append(values, val.List()...)
				}
				e.scope.Data[block.Label][key] = Value(values)
			}
		}
	}

	return Result{Status: 0}
}

func (e *Evaluator) evalBlock(block *Block) Result {
	for _, item := range block.Body {
		switch v := item.(type) {
		case Cmd:
			result := e.evalCommand(&v)
			if result.Error != nil || result.Halt {
				return result
			}
		case Pipeline:
			result := e.evalPipeline(&v)
			if result.Error != nil || result.Halt {
				return result
			}
		case Block:
			// Handle control structures specially
			if v.Type == CustomBlock {
				result := e.evalControlStructure(&v)
				if result.Error != nil || result.Halt {
					return result
				}
			} else {
				result := e.evalBlock(&v)
				if result.Error != nil || result.Halt {
					return result
				}
			}
		}
	}

	return Result{Status: 0}
}

func (e *Evaluator) evalControlStructure(block *Block) Result {
	switch block.Label {
	case "if":
		return e.evalIf(block)
	case "for":
		return e.evalFor(block)
	case "while":
		return e.evalWhile(block)
	default:
		// Treat unknown control structures as regular blocks
		return e.evalBlock(block)
	}
}

func (e *Evaluator) evalIf(block *Block) Result {
	// Execute condition (the arguments of the if statement)
	if len(block.Args) == 0 {
		return Result{Error: &BoxError{Message: "if: missing condition"}}
	}

	// Create a simple condition command
	conditionCmd := &Cmd{
		Verb:        block.Args[0],
		Args:        []Expr{},
		ErrorPolicy: FailFast,
	}

	// Add remaining args as arguments to the condition
	for i := 1; i < len(block.Args); i++ {
		arg := block.Args[i]
		if strings.HasPrefix(arg, "$") {
			name := strings.TrimPrefix(arg, "$")
			conditionCmd.Args = append(conditionCmd.Args, &VariableExpr{Name: name})
		} else {
			conditionCmd.Args = append(conditionCmd.Args, &LiteralExpr{Value: arg})
		}
	}

	condResult := e.evalCommand(conditionCmd)

	// If condition succeeds (status 0), execute if body
	if condResult.Status == 0 {
		for _, item := range block.Body {
			if nestedBlock, ok := item.(Block); ok && nestedBlock.Label == "else" {
				break // Skip else block
			}

			switch v := item.(type) {
			case Cmd:
				result := e.evalCommand(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// For if statements, propagate all halt types up
					return result
				}
			case Pipeline:
				result := e.evalPipeline(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// For if statements, propagate all halt types up
					return result
				}
			case Block:
				if v.Label != "else" {
					result := e.evalControlStructure(&v)
					if result.Error != nil {
						return result
					}
					if result.Halt {
						// For if statements, propagate all halt types up
						return result
					}
				}
			}
		}
	} else {
		// Execute else block if condition failed
		for _, item := range block.Body {
			if elseBlock, ok := item.(Block); ok && elseBlock.Label == "else" {
				return e.evalBlock(&elseBlock)
			}
		}
	}

	return Result{Status: 0}
}

func (e *Evaluator) evalFor(block *Block) Result {
	// Simple for loop implementation
	// for var in list...
	if len(block.Args) < 3 || block.Args[1] != "in" {
		return Result{Error: &BoxError{Message: "for: invalid syntax, expected 'for var in list'"}}
	}

	varName := block.Args[0]
	items := block.Args[2:]

	for _, item := range items {
		e.scope.Set(varName, Value{item})

		for _, bodyItem := range block.Body {
			switch v := bodyItem.(type) {
			case Cmd:
				result := e.evalCommand(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of for loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			case Pipeline:
				result := e.evalPipeline(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of for loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			case Block:
				result := e.evalControlStructure(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of for loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			}
		}
		nextIteration:
	}

	return Result{Status: 0}
}

func (e *Evaluator) evalWhile(block *Block) Result {
	for {
		// Execute condition
		if len(block.Args) == 0 {
			break
		}

		conditionCmd := &Cmd{
			Verb:        block.Args[0],
			Args:        []Expr{},
			ErrorPolicy: FailFast,
		}

		for i := 1; i < len(block.Args); i++ {
			conditionCmd.Args = append(conditionCmd.Args, &LiteralExpr{Value: block.Args[i]})
		}

		condResult := e.evalCommand(conditionCmd)
		if condResult.Status != 0 {
			break
		}

		// Execute body
		for _, bodyItem := range block.Body {
			switch v := bodyItem.(type) {
			case Cmd:
				result := e.evalCommand(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of while loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			case Pipeline:
				result := e.evalPipeline(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of while loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			case Block:
				result := e.evalControlStructure(&v)
				if result.Error != nil {
					return result
				}
				if result.Halt {
					// Handle different halt types
					switch result.HaltType {
					case BreakHalt:
						// Break out of while loop
						return Result{Status: result.Status}
					case ContinueHalt:
						// Continue to next iteration
						goto nextIteration
					case ReturnHalt, ExitHalt:
						// Return from function or exit program - propagate up
						return result
					}
				}
			}
		}
		nextIteration:
	}

	return Result{Status: 0}
}

func (e *Evaluator) callFunction(fn *Block, args []string) Result {
	return e.callFunctionWithNamespace(fn, args, "")
}

func (e *Evaluator) callFunctionWithNamespace(fn *Block, args []string, namespace string) Result {
	// Create new scope for function call
	childScope := e.scope.Child()
	// Copy parent's data to child scope but NOT functions to avoid recursion
	rootScope := e.getRootScope()
	for name, dataMap := range rootScope.Data {
		childScope.Data[name] = dataMap
	}
	
	// Set the current namespace context
	childScope.CurrentNamespace = namespace

	oldScope := e.scope
	e.scope = childScope
	defer func() { e.scope = oldScope }()

	// Set function arguments
	for i, argName := range fn.Args {
		if strings.Contains(argName, "=") {
			// Handle default values
			parts := strings.SplitN(argName, "=", 2)
			name := parts[0]
			defaultVal := parts[1]

			if i < len(args) {
				e.scope.Set(name, Value{args[i]})
			} else {
				e.scope.Set(name, Value{defaultVal})
			}
		} else {
			// Regular argument
			if i < len(args) {
				e.scope.Set(argName, Value{args[i]})
			}
		}
	}

	// Set remaining args as numbered parameters
	for i := len(fn.Args); i < len(args); i++ {
		e.scope.Set(strconv.Itoa(i+1), Value{args[i]})
	}

	result := e.evalBlock(fn)

	// Propagate all variables back to parent scope
	if oldScope != nil {
		for name, value := range e.scope.Variables {
			oldScope.Set(name, value)
		}
	}

	// Handle return from function properly
	if result.Halt && result.HaltType == ReturnHalt {
		// Return from function should not halt the caller
		result.Halt = false
		result.HaltType = NoHalt
	}
	// Other halt types (ExitHalt) should propagate up

	return result
}

// handleNonLocalFunction handles namespaced function calls and builtin commands
func (e *Evaluator) handleNonLocalFunction(cmd *Cmd, args []Value) Result {
	if strings.Contains(cmd.Verb, ".") {
		// Handle namespaced function calls like "util.helper"
		parts := strings.Split(cmd.Verb, ".")
		if len(parts) == 2 {
			namespace := parts[0]
			functionName := parts[1]

			if namespaceBlocks, exists := e.getRootScope().Namespaces[namespace]; exists {
				if fn, exists := namespaceBlocks[functionName]; exists {
					var strArgs []string
					for _, arg := range args {
						strArgs = append(strArgs, arg.String())
					}
					return e.callFunctionWithNamespace(fn, strArgs, namespace)
				} else {
					return Result{Error: &BoxError{Message: fmt.Sprintf("function '%s' not found in namespace '%s'", functionName, namespace)}}
				}
			} else {
				return Result{Error: &BoxError{Message: fmt.Sprintf("namespace '%s' not found", namespace)}}
			}
		} else {
			return Result{Error: &BoxError{Message: fmt.Sprintf("invalid namespaced function call: %s", cmd.Verb)}}
		}
	} else if builtin, ok := e.builtins[cmd.Verb]; ok {
		// Check for builtin
		return builtin(args, e.scope)
	} else {
		// Unknown command - fail with helpful error
		return Result{Error: &BoxError{
			Message: fmt.Sprintf("unknown command: %s", cmd.Verb),
			Location: Location{
				Filename: e.filename,
				Line:     cmd.Line,
				Column:   cmd.Column,
			},
			Help: fmt.Sprintf("'%s' is not a built-in verb. Box only supports internal commands.", cmd.Verb),
		}}
	}
}

func (e *Evaluator) evalCommand(cmd *Cmd) Result {
	// Evaluate arguments
	var args []Value
	for _, arg := range cmd.Args {
		val, err := e.evalExpression(arg)
		if err != nil {
			result := Result{Error: err}
			e.updateStatus(result)
			return result
		}
		args = append(args, val)
	}

	// Handle simple output redirections (>, >>, 2>)
	origStdout := os.Stdout
	origStderr := os.Stderr
	var stdoutFile *os.File
	var stderrFile *os.File

	for _, r := range cmd.Redirects {
		switch r.Type {
		case ">":
			f, err := os.OpenFile(r.Target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				res := Result{Error: &BoxError{Message: fmt.Sprintf("redirect: %v", err)}}
				e.updateStatus(res)
				return res
			}
			os.Stdout = f
			stdoutFile = f
		case ">>":
			f, err := os.OpenFile(r.Target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				res := Result{Error: &BoxError{Message: fmt.Sprintf("redirect: %v", err)}}
				e.updateStatus(res)
				return res
			}
			os.Stdout = f
			stdoutFile = f
		case "2>":
			f, err := os.OpenFile(r.Target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				res := Result{Error: &BoxError{Message: fmt.Sprintf("redirect: %v", err)}}
				e.updateStatus(res)
				return res
			}
			os.Stderr = f
			stderrFile = f
		}
	}

	var result Result

	// Check for function call first - look in root scope only to avoid recursion
	if fn, exists := e.getRootScope().Functions[cmd.Verb]; exists {
		var strArgs []string
		for _, arg := range args {
			strArgs = append(strArgs, arg.String())
		}
		result = e.callFunction(fn, strArgs)
	} else if e.scope.CurrentNamespace != "" {
		// Check for function in current namespace context
		if namespaceBlocks, exists := e.getRootScope().Namespaces[e.scope.CurrentNamespace]; exists {
			if fn, exists := namespaceBlocks[cmd.Verb]; exists {
				var strArgs []string
				for _, arg := range args {
					strArgs = append(strArgs, arg.String())
				}
				result = e.callFunction(fn, strArgs)
			} else {
				result = e.handleNonLocalFunction(cmd, args)
			}
		} else {
			result = e.handleNonLocalFunction(cmd, args)
		}
	} else {
		result = e.handleNonLocalFunction(cmd, args)
	}

	// Restore stdout/stderr after command execution
	if stdoutFile != nil {
		stdoutFile.Close()
		os.Stdout = origStdout
	}
	if stderrFile != nil {
		stderrFile.Close()
		os.Stderr = origStderr
	}

	// Update status after command execution
	e.updateStatus(result)

	// Spawn returns a PID; treat as success for control flow
	if cmd.Verb == "spawn" && result.Error == nil {
		result.Status = 0
	}

	// Handle error policies based on non-zero status or explicit error
	if result.Error != nil || result.Status != 0 {
		if cmd.ErrorPolicy == IgnoreError {
			// Suppress error, continue execution
			return Result{Status: result.Status}
		} else if cmd.ErrorPolicy == FallbackOnError && cmd.Fallback != nil {
			// Execute fallback and continue
			e.evalCommand(cmd.Fallback)
			// Return successful result to continue execution
			return Result{Status: 0}
		} else if cmd.ErrorPolicy == TryFallbackHalt && cmd.Fallback != nil {
			// Execute fallback and then halt, preserving original status
			e.evalCommand(cmd.Fallback)
			return Result{Status: result.Status, Halt: true}
		} else if cmd.ErrorPolicy == FailFast {
			// Fail-fast: non-zero exit aborts current scope
			result.Halt = true
			return result
		}
	}

	return result
}

func (e *Evaluator) updateStatus(result Result) {
	e.scope.Set("status", Value{strconv.Itoa(result.Status)})
}

func (e *Evaluator) getRootScope() *Scope {
	scope := e.scope
	for scope.Parent != nil {
		scope = scope.Parent
	}
	return scope
}

func (e *Evaluator) expandVariables(text string) string {
	// Handle command substitution $(...) patterns first
	result := text

	// Handle $(...) patterns
	for {
		start := strings.Index(result, "$(")
		if start == -1 {
			break
		}

		// Find matching closing parenthesis
		depth := 1
		end := start + 2
		for end < len(result) && depth > 0 {
			if result[end] == '(' {
				depth++
			} else if result[end] == ')' {
				depth--
			}
			if depth > 0 {
				end++
			}
		}

		if depth == 0 {
			commandStr := result[start+2 : end]

			// Execute command substitution
			value, err := e.executeCommandSubstitution(commandStr)
			var replacement string
			if err == nil {
				replacement = value.String()
			}

			result = result[:start] + replacement + result[end+1:]
		} else {
			break // Unmatched parentheses
		}
	}

	// Handle ${...} patterns
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		varPath := result[start+2 : end]
		var replacement string

		if strings.Contains(varPath, ".") {
			// Header lookup like ${config.name} or namespaced ${namespace.block.field}
			parts := strings.Split(varPath, ".")
			if len(parts) >= 2 {
				if len(parts) == 2 {
					// Simple case: ${block.field}
					blockName := parts[0]
					fieldName := parts[1]

					scope := e.scope
					for scope != nil {
						if block, ok := scope.Data[blockName]; ok {
							if val, ok := block[fieldName]; ok {
								replacement = val.String()
								break
							}
						}
						scope = scope.Parent
					}
				} else if len(parts) == 3 {
					// Namespaced case: ${namespace.block.field}
					namespace := parts[0]
					blockName := parts[1]
					fieldName := parts[2]

					// Look for namespaced data block
					namespacedBlockName := namespace + "." + blockName
					scope := e.scope
					for scope != nil {
						if block, ok := scope.Data[namespacedBlockName]; ok {
							if val, ok := block[fieldName]; ok {
								replacement = val.String()
								break
							}
						}
						scope = scope.Parent
					}
				}
			}
		} else {
			// Regular variable like ${var} or ${var[*]} or ${var[index]}
			if strings.Contains(varPath, "[") && strings.Contains(varPath, "]") {
				// Handle array syntax ${var[*]} or ${var[2]}
				bracketStart := strings.Index(varPath, "[")
				bracketEnd := strings.Index(varPath, "]")
				if bracketStart < bracketEnd {
					varName := varPath[:bracketStart]
					indexStr := varPath[bracketStart+1 : bracketEnd]

					if val, ok := e.scope.Get(varName); ok {
						if indexStr == "*" {
							// Return all elements joined with spaces
							replacement = strings.Join(val.List(), " ")
						} else if idx, err := strconv.Atoi(indexStr); err == nil {
							// Return specific index
							if idx >= 0 && idx < len(val.List()) {
								replacement = val.List()[idx]
							}
						}
					}
				}
			} else {
				// Simple variable like ${var}
				if val, ok := e.scope.Get(varPath); ok {
					replacement = val.String()
				}
			}
		}

		result = result[:start] + replacement + result[end+1:]
	}

	// Handle $var patterns (simpler case)
	for {
		start := strings.Index(result, "$")
		if start == -1 {
			break
		}

		// Find end of variable name
		end := start + 1
		for end < len(result) && (result[end] >= 'a' && result[end] <= 'z' ||
			result[end] >= 'A' && result[end] <= 'Z' ||
			result[end] >= '0' && result[end] <= '9' ||
			result[end] == '_') {
			end++
		}

		if end > start+1 {
			varName := result[start+1 : end]
			var replacement string

			if val, ok := e.scope.Get(varName); ok {
				replacement = val.String()
			}

			result = result[:start] + replacement + result[end:]
		} else {
			break
		}
	}

	return result
}

func (e *Evaluator) evalExpression(expr Expr) (Value, error) {
	switch v := expr.(type) {
	case *LiteralExpr:
		// Check if this literal contains variable expansions
		expanded := e.expandVariables(v.Value)
		return Value{expanded}, nil

	case *VariableExpr:
		val, ok := e.scope.Get(v.Name)
		if !ok {
			return Value{}, &BoxError{
				Message: fmt.Sprintf("undefined variable: %s", v.Name),
				Location: Location{
					Filename: e.filename,
					// Note: We don't have line/column info here as expressions don't carry location
				},
				Help: fmt.Sprintf("Variable '$%s' is not defined. Check spelling or use 'set %s value' to define it.", v.Name, v.Name),
			}
		}

		if v.Index != nil {
			if *v.Index == "*" {
				return val, nil
			}
			idx, err := strconv.Atoi(*v.Index)
			if err != nil {
				return Value{}, &BoxError{
					Message: fmt.Sprintf("invalid array index: %s", *v.Index),
				}
			}
			if idx < 0 || idx >= len(val) {
				return Value{}, nil
			}
			return Value{val[idx]}, nil
		}

		// Return first element for $var
		if len(val) > 0 {
			return Value{val[0]}, nil
		}
		return Value{}, nil

	case *BlockLookupExpr:
		parts := strings.Split(v.Path, ".")
		if len(parts) < 2 {
			return Value{}, &BoxError{
				Message: fmt.Sprintf("invalid header lookup: %s", v.Path),
			}
		}

		blockName := parts[0]
		fieldName := parts[1]

		// Look in current scope and parent scopes
		scope := e.scope
		for scope != nil {
			if block, ok := scope.Data[blockName]; ok {
				if val, ok := block[fieldName]; ok {
					return val, nil
				}
			}
			scope = scope.Parent
		}

		return Value{}, &BoxError{
			Message: fmt.Sprintf("undefined header field: %s", v.Path),
		}

	case *CommandSubExpr:
		// Execute the command and capture its output
		result, err := e.executeCommandSubstitution(v.Command)
		if err != nil {
			return Value{}, err
		}
		return result, nil

	default:
		return Value{}, &BoxError{
			Message: fmt.Sprintf("unknown expression type: %T", expr),
		}
	}
}

// executeCommandSubstitution parses and executes a command substitution
func (e *Evaluator) executeCommandSubstitution(commandStr string) (Value, error) {
	// Parse the command string as a mini Box program
	parser, err := NewParticleParser("command-substitution")
	if err != nil {
		return Value{}, &BoxError{
			Message: fmt.Sprintf("command substitution parser error: %v", err),
		}
	}

	program, err := parser.ParseString(commandStr)
	if err != nil {
		return Value{}, &BoxError{
			Message: fmt.Sprintf("command substitution parse error: %v", err),
		}
	}

	// Execute the program and capture its output
	if program.Main == nil || len(program.Main.Body) == 0 {
		return Value{""}, nil
	}

	// Create a child scope for the command substitution
	childScope := e.scope.Child()

	// Copy parent variables to child scope
	for name, value := range e.scope.Variables {
		childScope.Variables[name] = value
	}

	// Copy parent data to child scope
	rootScope := e.getRootScope()
	for name, dataMap := range rootScope.Data {
		childScope.Data[name] = dataMap
	}
	for name, fn := range rootScope.Functions {
		childScope.Functions[name] = fn
	}
	for name, blocks := range rootScope.Namespaces {
		childScope.Namespaces[name] = blocks
	}

	childEvaluator := NewEvaluator(childScope)

	// Capture stdout without forwarding to the parent
	originalStdout := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return Value{}, &BoxError{
			Message: fmt.Sprintf("command substitution pipe error: %v", err),
		}
	}

	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	// Execute the command
	result := childEvaluator.evalBlock(program.Main)

	// Close write end and restore stdout
	w.Close()
	os.Stdout = originalStdout
	<-done
	r.Close()

	if result.Error != nil {
		return Value{}, &BoxError{
			Message: fmt.Sprintf("command substitution execution error: %v", result.Error),
		}
	}

	// Process output - split by lines and trim
	outputStr := strings.TrimSpace(buf.String())
	if outputStr == "" {
		return Value{""}, nil
	}

	lines := strings.Split(outputStr, "\n")
	return Value(lines), nil
}

// evalPipeline executes a pipeline of commands and collects exit codes into $status
func (e *Evaluator) evalPipeline(pipeline *Pipeline) Result {
	if len(pipeline.Commands) == 0 {
		return Result{Status: 0}
	}

	if len(pipeline.Commands) == 1 {
		// Single command - set status as single-element array
		result := e.evalCommand(&pipeline.Commands[0])
		e.scope.Set("status", Value{strconv.Itoa(result.Status)})
		return result
	}

	// Create pipes between commands
	var pipes []*os.File
	var readers []*os.File

	// Create n-1 pipes for n commands
	for i := 0; i < len(pipeline.Commands)-1; i++ {
		r, w, err := os.Pipe()
		if err != nil {
			return Result{Error: &BoxError{
				Message: fmt.Sprintf("pipeline: failed to create pipe: %v", err),
			}}
		}
		readers = append(readers, r)
		pipes = append(pipes, w)
	}

	// Save original stdin/stdout
	originalStdin := os.Stdin
	originalStdout := os.Stdout

	// Collect exit codes for each command in pipeline
	var exitCodes []string
	var lastResult Result

	// Execute commands in pipeline
	for i, cmd := range pipeline.Commands {
		// Set up stdin for this command
		if i > 0 {
			os.Stdin = readers[i-1]
		}

		// Set up stdout for this command
		if i < len(pipeline.Commands)-1 {
			os.Stdout = pipes[i]
		} else {
			// Last command outputs to original stdout
			os.Stdout = originalStdout
		}

		// Execute the command
		result := e.evalCommand(&cmd)
		lastResult = result

		// Collect exit code (spec: "Each element is a decimal integer")
		exitCodes = append(exitCodes, strconv.Itoa(result.Status))

		// Close the write end of the pipe for this command
		if i < len(pipeline.Commands)-1 {
			pipes[i].Close()
		}

		// Continue pipeline execution even if commands fail (collect all exit codes)
		// Only stop on critical errors
		if result.Error != nil {
			// Restore original stdin/stdout
			os.Stdin = originalStdin
			os.Stdout = originalStdout

			// Close remaining pipes
			for j := i; j < len(pipes); j++ {
				pipes[j].Close()
			}
			for j := i; j < len(readers); j++ {
				readers[j].Close()
			}

			// Set collected exit codes in $status before returning error
			e.scope.Set("status", Value(exitCodes))
			return result
		}
	}

	// Restore original stdin/stdout
	os.Stdin = originalStdin
	os.Stdout = originalStdout

	// Close all remaining readers
	for _, r := range readers {
		r.Close()
	}

	// Set pipeline exit codes in $status array as per spec
	// "$status = (exit1 exit2 exit3)"
	e.scope.Set("status", Value(exitCodes))

	// Return the status of the last command in the pipeline
	return lastResult
}
