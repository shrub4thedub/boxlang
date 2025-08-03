package box

import (
	"fmt"
	"os"
	"strings"
)

type Location struct {
	Filename string
	Line     int
	Column   int
}

type BoxError struct {
	Message  string
	Location Location
	Help     string
	Code     string
}

func (e *BoxError) Error() string {
	if e.Location.Filename != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.Location.Filename, e.Location.Line, e.Location.Column, e.Message)
	}
	return e.Message
}

func FormatError(err *BoxError) string {
	var b strings.Builder
	
	b.WriteString("✗ ")
	b.WriteString(err.Message)
	b.WriteString("\n")
	
	if err.Location.Filename != "" {
		b.WriteString(fmt.Sprintf("  ╭─[%s:%d:%d]\n", err.Location.Filename, err.Location.Line, err.Location.Column))
		
		// Try to read source context
		sourceLines := readSourceContext(err.Location.Filename, err.Location.Line)
		if len(sourceLines) > 0 {
			b.WriteString("  │\n")
			
			// Show context lines (up to 2 before, target line, up to 1 after)
			startLine := err.Location.Line - 2
			if startLine < 1 {
				startLine = 1
			}
			
			for i, line := range sourceLines {
				lineNum := startLine + i
				if lineNum == err.Location.Line {
					// Highlight error line
					b.WriteString(fmt.Sprintf("%3d│ %s\n", lineNum, line))
					b.WriteString("  │ ")
					
					// Add pointer to column
					for j := 0; j < err.Location.Column-1; j++ {
						if j < len(line) && line[j] == '\t' {
							b.WriteString("\t")
						} else {
							b.WriteString(" ")
						}
					}
					b.WriteString("─┬─ here\n")
					b.WriteString("  │ ")
					for j := 0; j < err.Location.Column-1; j++ {
						if j < len(line) && line[j] == '\t' {
							b.WriteString("\t")
						} else {
							b.WriteString(" ")
						}
					}
					b.WriteString(" ╰─ ")
					b.WriteString(err.Message)
					b.WriteString("\n")
				} else {
					// Context line
					b.WriteString(fmt.Sprintf("%3d│ %s\n", lineNum, line))
				}
			}
		} else if err.Code != "" {
			// Fallback to provided code
			b.WriteString("  │\n")
			b.WriteString(fmt.Sprintf("%3d│ %s\n", err.Location.Line, err.Code))
			b.WriteString("  │ ")
			
			// Add pointer to column
			for i := 0; i < err.Location.Column-1; i++ {
				b.WriteString(" ")
			}
			b.WriteString("─┬─ here\n")
			b.WriteString("  │ ")
			for i := 0; i < err.Location.Column-1; i++ {
				b.WriteString(" ")
			}
			b.WriteString(" ╰─ ")
			b.WriteString(err.Message)
			b.WriteString("\n")
		}
		
		b.WriteString("  │\n")
		
		if err.Help != "" {
			b.WriteString("  │ 💡 Help: ")
			b.WriteString(err.Help)
			b.WriteString("\n")
			b.WriteString("  │\n")
		}
	}
	
	return b.String()
}

// readSourceContext reads lines around the error location from the source file
func readSourceContext(filename string, targetLine int) []string {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil
	}
	
	lines := strings.Split(string(content), "\n")
	if targetLine < 1 || targetLine > len(lines) {
		return nil
	}
	
	// Return up to 5 lines of context (2 before, target, 2 after)
	start := targetLine - 3 // 1-based to 0-based, then -2 for context
	if start < 0 {
		start = 0
	}
	
	end := targetLine + 2 // 1-based + 1 for context + 1 for exclusive end
	if end > len(lines) {
		end = len(lines)
	}
	
	return lines[start:end]
}