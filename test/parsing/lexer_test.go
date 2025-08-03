package parsing

import (
	"testing"

	"box/internal/box"
)

func TestLexerBasicTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []box.TokenKind
	}{
		{
			name:  "simple words",
			input: "echo hello world",
			expected: []box.TokenKind{
				box.WORD, box.WORD, box.WORD, box.EOF,
			},
		},
		{
			name:  "quoted strings",
			input: `echo "hello world" 'raw string'`,
			expected: []box.TokenKind{
				box.WORD, box.DOUBLE_QUOTE, box.SINGLE_QUOTE, box.EOF,
			},
		},
		{
			name:  "variables",
			input: "echo $var ${array[*]} ${config.field}",
			expected: []box.TokenKind{
				box.WORD, box.VARIABLE, box.VARIABLE, box.HEADER_LOOKUP, box.EOF,
			},
		},
		{
			name:  "error handling",
			input: "cmd ? echo fallback",
			expected: []box.TokenKind{
				box.WORD, box.IGNORE_ERROR, box.WORD, box.WORD, box.EOF,
			},
		},
		{
			name:  "blocks",
			input: "[main] echo hello end",
			expected: []box.TokenKind{
				box.HEADER_START, box.WORD, box.WORD, box.BLOCK_END, box.EOF,
			},
		},
		{
			name:  "redirections",
			input: "cmd > file 2> error.log",
			expected: []box.TokenKind{
				box.WORD, box.REDIRECT, box.WORD, box.REDIRECT, box.WORD, box.EOF,
			},
		},
		{
			name:  "numbers fixed",
			input: "version 2.0 count 123",
			expected: []box.TokenKind{
				box.WORD, box.WORD, box.WORD, box.WORD, box.EOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := box.NewLexer(tt.input, "test")
			var tokens []box.TokenKind

			for {
				token := lexer.NextToken()
				tokens = append(tokens, token.Kind)
				if token.Kind == box.EOF {
					break
				}
			}

			if len(tokens) != len(tt.expected) {
				t.Errorf("Expected %d tokens, got %d", len(tt.expected), len(tokens))
				return
			}

			for i, expected := range tt.expected {
				if tokens[i] != expected {
					t.Errorf("Token %d: expected %v, got %v", i, expected, tokens[i])
				}
			}
		})
	}
}

func TestLexerComments(t *testing.T) {
	input := `# This is a comment
echo hello # inline comment
# Another comment`

	lexer := box.NewLexer(input, "test")
	var tokens []box.Token

	for {
		token := lexer.NextToken()
		tokens = append(tokens, token)
		if token.Kind == box.EOF {
			break
		}
	}

	expectedKinds := []box.TokenKind{
		box.COMMENT, box.WORD, box.WORD, box.COMMENT, box.COMMENT, box.EOF,
	}

	if len(tokens) != len(expectedKinds) {
		t.Errorf("Expected %d tokens, got %d", len(expectedKinds), len(tokens))
		return
	}

	for i, expected := range expectedKinds {
		if tokens[i].Kind != expected {
			t.Errorf("Token %d: expected %v, got %v", i, expected, tokens[i].Kind)
		}
	}
}

func TestLexerNumbers(t *testing.T) {
	// Test the fix for the '2' being interpreted as '?' bug
	input := "2 2.0 2.5 12345"
	lexer := box.NewLexer(input, "test")
	
	var tokens []box.Token
	for {
		token := lexer.NextToken()
		tokens = append(tokens, token)
		if token.Kind == box.EOF {
			break
		}
	}
	
	expectedValues := []string{"2", "2.0", "2.5", "12345"}
	
	if len(tokens)-1 != len(expectedValues) { // -1 for EOF
		t.Errorf("Expected %d number tokens, got %d", len(expectedValues), len(tokens)-1)
		return
	}
	
	for i, expectedValue := range expectedValues {
		if tokens[i].Kind != box.WORD {
			t.Errorf("Token %d: expected WORD, got %v", i, tokens[i].Kind)
		}
		if tokens[i].Value != expectedValue {
			t.Errorf("Token %d: expected value '%s', got '%s'", i, expectedValue, tokens[i].Value)
		}
	}
}