package main

import (
	"fmt"
	"os"
	"strings"

	"box/internal/box"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  box <script.box> [args...]  - Run a box script")
		fmt.Println("  box lex <script.box>        - Debug lexer output")
		fmt.Println("  box ast <script.box>        - Debug parser AST")
		os.Exit(1)
	}

	if os.Args[1] == "lex" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: box lex <script.box>")
			os.Exit(1)
		}
		lexDebug(os.Args[2])
		return
	}

	if os.Args[1] == "ast" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: box ast <script.box>")
			os.Exit(1)
		}
		astDebug(os.Args[2])
		return
	}

	scriptPath := os.Args[1]
	args := os.Args[2:]
	
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	lexer := box.NewLexer(string(content), scriptPath)
	parser := box.NewParser(lexer)
	
	program, err := parser.Parse()
	if err != nil {
		if boxErr, ok := err.(*box.BoxError); ok {
			fmt.Print(box.FormatError(boxErr))
		} else {
			fmt.Printf("Parse error: %v\n", err)
		}
		os.Exit(1)
	}

	scope := box.NewScope()
	evaluator := box.NewEvaluatorWithFilename(scope, scriptPath)
	
	result := evaluator.Eval(program, args)
	if result.Error != nil {
		if boxErr, ok := result.Error.(*box.BoxError); ok {
			fmt.Print(box.FormatError(boxErr))
		} else {
			fmt.Printf("Runtime error: %v\n", result.Error)
		}
		os.Exit(1)
	}

	os.Exit(result.Status)
}

func lexDebug(filename string) {
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	lexer := box.NewLexer(string(content), filename)
	
	fmt.Printf("📄 Lexing: %s\n", filename)
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Printf("%-4s %-3s %-15s %-20s %s\n", "Line", "Col", "Kind", "Value", "Raw")
	fmt.Println("─────────────────────────────────────────────────────────────────")
	
	tokenCount := 0
	for {
		token := lexer.NextToken()
		if token.Kind == box.EOF {
			break
		}
		
		// Skip comments for cleaner output unless they're interesting
		if token.Kind == box.COMMENT {
			continue
		}
		
		tokenCount++
		
		// Format value for display
		displayValue := token.Value
		if len(displayValue) > 18 {
			displayValue = displayValue[:15] + "..."
		}
		
		// Show raw representation  
		raw := token.Value
		if token.Kind == box.SINGLE_QUOTE {
			raw = "'" + token.Value + "'"
		} else if token.Kind == box.DOUBLE_QUOTE {
			raw = "\"" + token.Value + "\""
		} else if token.Kind == box.COMMAND_SUB {
			if !strings.HasPrefix(token.Value, "$(") {
				raw = "`" + token.Value + "`"
			} else {
				raw = "$(" + token.Value + ")"
			}
		} else if token.Kind == box.VARIABLE {
			raw = "$" + token.Value
		} else if token.Kind == box.HEADER_LOOKUP {
			raw = "${" + token.Value + "}"
		} else if token.Kind == box.HEADER_START {
			raw = token.Value
		}
		
		fmt.Printf("%-4d %-3d %-15s %-20s %s\n", 
			token.Line, token.Column, token.Kind.String(), displayValue, raw)
	}
	
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Printf("✅ Lexed %d tokens successfully\n", tokenCount)
}

func astDebug(filename string) {
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	lexer := box.NewLexer(string(content), filename)
	parser := box.NewParser(lexer)
	
	program, err := parser.Parse()
	if err != nil {
		if boxErr, ok := err.(*box.BoxError); ok {
			fmt.Print(box.FormatError(boxErr))
		} else {
			fmt.Printf("Parse error: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Printf("🌲 Abstract Syntax Tree: %s\n", filename)
	fmt.Println("═════════════════════════════════════════════════════════════════")
	
	// Print program summary
	fmt.Printf("📊 Program Summary:\n")
	fmt.Printf("  Functions: %d\n", len(program.Functions))
	fmt.Printf("  Data blocks: %d\n", len(program.Data))
	fmt.Printf("  Main block: %v\n", program.Main != nil)
	fmt.Printf("  Total blocks: %d\n", len(program.Blocks))
	fmt.Println()

	// Print functions
	if len(program.Functions) > 0 {
		fmt.Println("🔧 Functions:")
		for name, fn := range program.Functions {
			fmt.Printf("  [fn %s", name)
			if len(fn.Args) > 0 {
				fmt.Printf(" %s", strings.Join(fn.Args, " "))
			}
			fmt.Printf("]")
			if len(fn.Modifiers) > 0 {
				fmt.Printf(" (modifiers:")
				for _, mod := range fn.Modifiers {
					fmt.Printf(" %s", mod.Flag)
				}
				fmt.Printf(")")
			}
			fmt.Printf(" - %d statements\n", len(fn.Body))
			printBlockBody(fn.Body, "    ")
		}
		fmt.Println()
	}

	// Print data blocks
	if len(program.Data) > 0 {
		fmt.Println("📦 Data Blocks:")
		for name, data := range program.Data {
			fmt.Printf("  [data %s]", name)
			if len(data.Modifiers) > 0 {
				fmt.Printf(" (modifiers:")
				for _, mod := range data.Modifiers {
					fmt.Printf(" %s", mod.Flag)
				}
				fmt.Printf(")")
			}
			fmt.Printf(" - %d entries\n", len(data.Body))
			printBlockBody(data.Body, "    ")
		}
		fmt.Println()
	}

	// Print main block
	if program.Main != nil {
		fmt.Println("🏠 Main Block:")
		fmt.Printf("  %d statements\n", len(program.Main.Body))
		printBlockBody(program.Main.Body, "  ")
		fmt.Println()
	}

	fmt.Println("═════════════════════════════════════════════════════════════════")
	fmt.Printf("✅ Parsed successfully into %d top-level blocks\n", len(program.Blocks))
}

func printBlockBody(body []interface{}, indent string) {
	for i, item := range body {
		switch v := item.(type) {
		case box.Cmd:
			fmt.Printf("%s%d. %s", indent, i+1, formatCommand(&v))
		case box.Pipeline:
			fmt.Printf("%s%d. PIPELINE (%d commands)\n", indent, i+1, len(v.Commands))
			for j, cmd := range v.Commands {
				fmt.Printf("%s  %d: %s", indent, j+1, formatCommand(&cmd))
			}
		case box.Block:
			fmt.Printf("%s%d. [%s", indent, i+1, formatBlockType(v.Type))
			if v.Label != "" {
				fmt.Printf(" %s", v.Label)
			}
			fmt.Printf("] (%d items)\n", len(v.Body))
			printBlockBody(v.Body, indent+"  ")
		}
	}
}

func formatCommand(cmd *box.Cmd) string {
	result := cmd.Verb
	for _, arg := range cmd.Args {
		result += " " + formatExpression(arg)
	}
	
	if len(cmd.Redirects) > 0 {
		for _, r := range cmd.Redirects {
			result += " " + r.Type + " " + r.Target
		}
	}
	
	switch cmd.ErrorPolicy {
	case box.IgnoreError:
		result += " ?"
	case box.FallbackOnError:
		result += " ? " + formatCommand(cmd.Fallback)
	case box.TryFallbackHalt:
		result += " ! " + formatCommand(cmd.Fallback)
	}
	
	return result + "\n"
}

func formatExpression(expr box.Expr) string {
	switch v := expr.(type) {
	case *box.LiteralExpr:
		return v.Value
	case *box.VariableExpr:
		if v.Index != nil {
			return fmt.Sprintf("${%s[%s]}", v.Name, *v.Index)
		}
		return "$" + v.Name
	case *box.HeaderLookupExpr:
		return "${" + v.Path + "}"
	case *box.CommandSubExpr:
		return "`" + v.Command + "`"
	default:
		return fmt.Sprintf("<%T>", expr)
	}
}

func formatBlockType(blockType box.BlockType) string {
	switch blockType {
	case box.MainBlock:
		return "main"
	case box.FnBlock:
		return "fn"
	case box.DataBlock:
		return "data"
	case box.CustomBlock:
		return "custom"
	default:
		return "unknown"
	}
}