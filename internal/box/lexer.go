package box

import (
	"os"
	"strings"
	"unicode"
)

type TokenKind int

const (
	EOF TokenKind = iota
	WORD
	SINGLE_QUOTE
	DOUBLE_QUOTE
	COMMAND_SUB
	VARIABLE
	HEADER_LOOKUP
	REDIRECT
	PIPELINE
	IGNORE_ERROR
	HEADER_START
	BLOCK_END
	COMMENT
)

func (tk TokenKind) String() string {
	switch tk {
	case EOF:
		return "EOF"
	case WORD:
		return "WORD"
	case SINGLE_QUOTE:
		return "SINGLE_QUOTE"
	case DOUBLE_QUOTE:
		return "DOUBLE_QUOTE"
	case COMMAND_SUB:
		return "COMMAND_SUB"
	case VARIABLE:
		return "VARIABLE"
	case HEADER_LOOKUP:
		return "HEADER_LOOKUP"
	case REDIRECT:
		return "REDIRECT"
	case PIPELINE:
		return "PIPELINE"
	case IGNORE_ERROR:
		return "IGNORE_ERROR"
	case HEADER_START:
		return "HEADER_START"
	case BLOCK_END:
		return "BLOCK_END"
	case COMMENT:
		return "COMMENT"
	default:
		return "UNKNOWN"
	}
}

type Token struct {
	Kind   TokenKind
	Value  string
	Line   int
	Column int
}

type Lexer struct {
	input    string
	filename string
	pos      int
	line     int
	column   int
	current  rune
}

func NewLexer(input, filename string) *Lexer {
	l := &Lexer{
		input:    input,
		filename: filename,
		line:     1,
		column:   1,
	}
	if len(input) > 0 {
		l.current = rune(input[0])
	}
	return l
}

func NewLexerFromFile(filename string) (*Lexer, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return NewLexer(string(content), filename), nil
}

func (l *Lexer) advance() {
	if l.current == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}

	l.pos++
	if l.pos >= len(l.input) {
		l.current = 0
		return
	}

	l.current = rune(l.input[l.pos])
}

func (l *Lexer) peek() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return rune(l.input[l.pos+1])
}

func (l *Lexer) skipWhitespace() {
	for unicode.IsSpace(l.current) && l.current != 0 {
		l.advance()
	}
}

func (l *Lexer) readWord() string {
	start := l.pos
	for l.current != 0 && !unicode.IsSpace(l.current) &&
		!strings.ContainsRune("|>?[]#'\"$`", l.current) {
		l.advance()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readGlobPattern() string {
	start := l.pos
	for l.current != 0 && !unicode.IsSpace(l.current) &&
		l.current != '|' && l.current != '>' && l.current != '?' &&
		l.current != ']' && l.current != '#' {
		l.advance()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readUntilNewline() string {
	start := l.pos
	for l.current != '\n' && l.current != 0 {
		l.advance()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readSingleQuote() string {
	l.advance() // skip opening '
	start := l.pos
	for l.current != '\'' && l.current != 0 {
		l.advance()
	}
	result := l.input[start:l.pos]
	if l.current == '\'' {
		l.advance() // skip closing '
	}
	return result
}

func (l *Lexer) readDoubleQuote() string {
	l.advance() // skip opening "
	var result strings.Builder

	for l.current != '"' && l.current != 0 {
		if l.current == '\\' {
			l.advance()
			switch l.current {
			case 'n':
				result.WriteRune('\n')
			case 't':
				result.WriteRune('\t')
			case 'r':
				result.WriteRune('\r')
			case '\\':
				result.WriteRune('\\')
			case '"':
				result.WriteRune('"')
			default:
				result.WriteRune(l.current)
			}
		} else {
			result.WriteRune(l.current)
		}
		l.advance()
	}

	if l.current == '"' {
		l.advance() // skip closing "
	}
	return result.String()
}

func (l *Lexer) readCommandSub() string {
	if l.current == '`' {
		l.advance() // skip opening `
		start := l.pos
		for l.current != '`' && l.current != 0 {
			l.advance()
		}
		result := l.input[start:l.pos]
		if l.current == '`' {
			l.advance() // skip closing `
		}
		return result
	} else if l.current == '$' && l.peek() == '(' {
		l.advance() // skip $
		l.advance() // skip (
		start := l.pos
		depth := 1
		for depth > 0 && l.current != 0 {
			if l.current == '(' {
				depth++
			} else if l.current == ')' {
				depth--
			}
			if depth > 0 {
				l.advance()
			}
		}
		result := l.input[start:l.pos]
		if l.current == ')' {
			l.advance() // skip closing )
		}
		return result
	}
	return ""
}

func (l *Lexer) readVariable() string {
	l.advance() // skip $
	if l.current == '{' {
		l.advance() // skip {
		start := l.pos
		for l.current != '}' && l.current != 0 {
			l.advance()
		}
		result := l.input[start:l.pos]
		if l.current == '}' {
			l.advance() // skip }
		}
		return result
	} else {
		start := l.pos
		for unicode.IsLetter(l.current) || unicode.IsDigit(l.current) || l.current == '_' {
			l.advance()
		}
		result := l.input[start:l.pos]
		// Handle array access like $files[*]
		if l.current == '[' {
			for l.current != ']' && l.current != 0 {
				l.advance()
			}
			if l.current == ']' {
				l.advance()
			}
			return l.input[start:l.pos]
		}
		return result
	}
}

func (l *Lexer) readComment() string {
	l.advance() // skip #
	start := l.pos
	for l.current != '\n' && l.current != 0 {
		l.advance()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	line := l.line
	column := l.column

	switch l.current {
	case 0:
		return Token{EOF, "", line, column}
	case '#':
		value := l.readComment()
		return Token{COMMENT, value, line, column}
	case '\'':
		value := l.readSingleQuote()
		return Token{SINGLE_QUOTE, value, line, column}
	case '"':
		value := l.readDoubleQuote()
		return Token{DOUBLE_QUOTE, value, line, column}
	case '`':
		value := l.readCommandSub()
		return Token{COMMAND_SUB, value, line, column}
	case '$':
		if l.peek() == '(' {
			value := l.readCommandSub()
			return Token{COMMAND_SUB, value, line, column}
		} else {
			value := l.readVariable()
			if strings.Contains(value, ".") {
				return Token{HEADER_LOOKUP, value, line, column}
			}
			return Token{VARIABLE, value, line, column}
		}
	case '|':
		l.advance()
		return Token{PIPELINE, "|", line, column}
	case '>':
		l.advance()
		if l.current == '>' {
			l.advance()
			return Token{REDIRECT, ">>", line, column}
		}
		return Token{REDIRECT, ">", line, column}
	case '2':
		if l.peek() == '>' {
			l.advance() // skip 2
			l.advance() // skip >
			return Token{REDIRECT, "2>", line, column}
		}
		// If '2' is not followed by '>', treat it as a regular word
		value := l.readWord()
		return Token{WORD, value, line, column}
	case '?':
		l.advance()
		return Token{IGNORE_ERROR, "?", line, column}
	case '[':
		start := l.pos
		for l.current != ']' && l.current != 0 {
			l.advance()
		}
		if l.current == ']' {
			l.advance()
		}
		value := l.input[start:l.pos]
		return Token{HEADER_START, value, line, column}
	default:
		// Treat any sequence of non-special, non-whitespace characters as a word.
		// This allows paths like /dev/null or names with punctuation to be lexed
		// correctly as single tokens. Only reserved characters are handled by the
		// earlier cases in this switch.
		value := l.readWord()
		if value == "end" {
			return Token{BLOCK_END, value, line, column}
		}
		return Token{WORD, value, line, column}
	}
}

// TokenStream provides a streaming interface for tokens
func (l *Lexer) TokenStream() <-chan Token {
	ch := make(chan Token)
	go func() {
		defer close(ch)
		for {
			token := l.NextToken()
			ch <- token
			if token.Kind == EOF {
				break
			}
		}
	}()
	return ch
}
