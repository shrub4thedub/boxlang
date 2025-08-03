package test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCase represents a single test case for Box shell
type TestCase struct {
	Name       string // Test name
	Script     string // .box script content
	Args       []string // Command line arguments
	Stdin      string // Input to provide
	ExitCode   int    // Expected exit code
	Stdout     string // Expected stdout content
	Stderr     string // Expected stderr content
	ShouldFail bool   // Whether test should fail
}

// RunBoxTest executes a Box script and validates the results
func RunBoxTest(t *testing.T, testCase TestCase) {
	t.Helper()
	
	// Create temporary .box file
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test.box")
	
	err := os.WriteFile(scriptPath, []byte(testCase.Script), 0644)
	if err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}
	
	// Build command with args
	cmdArgs := []string{scriptPath}
	cmdArgs = append(cmdArgs, testCase.Args...)
	
	// Get working directory and find Box binary
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	
	// Try multiple paths to find the box binary
	boxPaths := []string{
		"../box",           // From test subdirectory
		"../../box",        // From test/runtime subdirectory
		"./box",            // From project root
	}
	
	var boxPath string
	for _, path := range boxPaths {
		absPath := filepath.Join(wd, path)
		if _, err := os.Stat(absPath); err == nil {
			boxPath = absPath
			break
		}
	}
	
	if boxPath == "" {
		t.Fatalf("Could not find box binary. Tried paths: %v from working directory: %s", boxPaths, wd)
	}
	
	cmd := exec.Command(boxPath, cmdArgs...)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	if testCase.Stdin != "" {
		cmd.Stdin = strings.NewReader(testCase.Stdin)
	}
	
	err = cmd.Run()
	
	// Check exit code
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Failed to execute Box script: %v", err)
		}
	}
	
	if exitCode != testCase.ExitCode {
		t.Errorf("Expected exit code %d, got %d", testCase.ExitCode, exitCode)
	}
	
	// Check stdout
	if testCase.Stdout != "" {
		actualStdout := strings.TrimSpace(stdout.String())
		expectedStdout := strings.TrimSpace(testCase.Stdout)
		if actualStdout != expectedStdout {
			t.Errorf("Stdout mismatch:\nExpected:\n%s\n\nActual:\n%s", expectedStdout, actualStdout)
		}
	}
	
	// Check stderr
	if testCase.Stderr != "" {
		actualStderr := strings.TrimSpace(stderr.String())
		expectedStderr := strings.TrimSpace(testCase.Stderr)
		if !strings.Contains(actualStderr, expectedStderr) {
			t.Errorf("Stderr mismatch:\nExpected to contain:\n%s\n\nActual:\n%s", expectedStderr, actualStderr)
		}
	}
	
	// Log output for debugging
	if testing.Verbose() {
		fmt.Printf("=== Test: %s ===\n", testCase.Name)
		fmt.Printf("Exit Code: %d\n", exitCode)
		fmt.Printf("Stdout:\n%s\n", stdout.String())
		fmt.Printf("Stderr:\n%s\n", stderr.String())
		fmt.Println("=================")
	}
}

// LoadTestDataFile loads a test file from testdata directory
func LoadTestDataFile(filename string) (string, error) {
	content, err := os.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ParseTestCase parses a test case from a structured comment format
func ParseTestCase(content string) *TestCase {
	lines := strings.Split(content, "\n")
	testCase := &TestCase{}
	
	var scriptLines []string
	var mode string
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		if strings.HasPrefix(line, "# TEST:") {
			testCase.Name = strings.TrimSpace(strings.TrimPrefix(line, "# TEST:"))
		} else if strings.HasPrefix(line, "# EXPECT_EXIT:") {
			fmt.Sscanf(line, "# EXPECT_EXIT: %d", &testCase.ExitCode)
		} else if strings.HasPrefix(line, "# EXPECT_STDOUT:") {
			mode = "stdout"
		} else if strings.HasPrefix(line, "# EXPECT_STDERR:") {
			mode = "stderr"
		} else if strings.HasPrefix(line, "# STDIN:") {
			mode = "stdin"
		} else if strings.HasPrefix(line, "# ARGS:") {
			argsStr := strings.TrimSpace(strings.TrimPrefix(line, "# ARGS:"))
			if argsStr != "" {
				testCase.Args = strings.Fields(argsStr)
			}
		} else if strings.HasPrefix(line, "# END_") {
			mode = ""
		} else if strings.HasPrefix(line, "#") && mode != "" {
			content := strings.TrimPrefix(line, "#")
			content = strings.TrimSpace(content)
			switch mode {
			case "stdout":
				if testCase.Stdout != "" {
					testCase.Stdout += "\n"
				}
				testCase.Stdout += content
			case "stderr":
				if testCase.Stderr != "" {
					testCase.Stderr += "\n"
				}
				testCase.Stderr += content
			case "stdin":
				if testCase.Stdin != "" {
					testCase.Stdin += "\n"
				}
				testCase.Stdin += content
			}
		} else if !strings.HasPrefix(line, "#") {
			scriptLines = append(scriptLines, line)
		}
	}
	
	testCase.Script = strings.Join(scriptLines, "\n")
	return testCase
}