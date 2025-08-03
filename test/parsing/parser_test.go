package parsing

import (
	"testing"

	"box/internal/box"
)

func TestParserBasicBlocks(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		checkFunc   func(*testing.T, *box.Program)
	}{
		{
			name:  "main block",
			input: `[main]\necho hello\nend`,
			checkFunc: func(t *testing.T, p *box.Program) {
				if p.Main == nil {
					t.Error("Expected main block, got nil")
				}
				if len(p.Main.Body) != 1 {
					t.Errorf("Expected 1 command in main, got %d", len(p.Main.Body))
				}
			},
		},
		{
			name:  "function block",
			input: `[fn greet name]\necho hello $name\nend`,
			checkFunc: func(t *testing.T, p *box.Program) {
				if len(p.Functions) != 1 {
					t.Errorf("Expected 1 function, got %d", len(p.Functions))
				}
				if _, ok := p.Functions["greet"]; !ok {
					t.Error("Expected function 'greet' not found")
				}
			},
		},
		{
			name:  "data block",
			input: `[data config]\nname test\nversion 1.0\nend`,
			checkFunc: func(t *testing.T, p *box.Program) {
				if len(p.Data) != 1 {
					t.Errorf("Expected 1 data block, got %d", len(p.Data))
				}
				if _, ok := p.Data["config"]; !ok {
					t.Error("Expected data block 'config' not found")
				}
			},
		},
		{
			name:  "function with modifiers",
			input: `[fn -i main_func]\necho hello\nend`,
			checkFunc: func(t *testing.T, p *box.Program) {
				fn, ok := p.Functions["main_func"]
				if !ok {
					t.Error("Function 'main_func' not found")
					return
				}
				if len(fn.Modifiers) != 1 || fn.Modifiers[0].Flag != "-i" {
					t.Error("Expected function to have -i modifier")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := box.NewLexer(tt.input, "test")
			parser := box.NewParser(lexer)
			
			program, err := parser.Parse()
			
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			
			if tt.checkFunc != nil {
				tt.checkFunc(t, program)
			}
		})
	}
}

func TestParserErrorHandling(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "fallback with ?",
			input: `echo hello ? echo fallback`,
		},
		{
			name:  "try-fallback-halt with !",
			input: `run cmd ! echo "failed"`,
		},
		{
			name:  "ignore error",
			input: `run badcmd ?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := box.NewLexer(tt.input, "test")
			parser := box.NewParser(lexer)
			
			program, err := parser.Parse()
			if err != nil {
				t.Errorf("Unexpected error parsing %s: %v", tt.name, err)
				return
			}
			
			if program.Main == nil || len(program.Main.Body) == 0 {
				t.Error("Expected commands in main block")
			}
		})
	}
}

func TestParserImports(t *testing.T) {
	input := `import utils.box
import math.box

[main]
echo hello
end`

	lexer := box.NewLexer(input, "test")
	parser := box.NewParser(lexer)
	
	// Note: This will fail because the files don't exist, but we can test parsing
	_, err := parser.Parse()
	
	// We expect an error because the import files don't exist
	if err == nil {
		t.Error("Expected error for missing import files")
	}
	
	// Check that the error is related to import, not parsing
	if err != nil && !contains(err.Error(), "import") {
		t.Errorf("Expected import-related error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || 
		   (len(s) > len(substr) && contains(s[1:], substr))
}

func TestParserVariableExpansion(t *testing.T) {
	input := `[main]
echo $var
echo ${array[*]}
echo ${config.field}
end`

	lexer := box.NewLexer(input, "test")
	parser := box.NewParser(lexer)
	
	program, err := parser.Parse()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}
	
	if program.Main == nil || len(program.Main.Body) != 3 {
		t.Errorf("Expected 3 commands in main block, got %d", len(program.Main.Body))
	}
}