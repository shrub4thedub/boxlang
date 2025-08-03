package box

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BuiltinFunc represents a built-in verb function
type BuiltinFunc func(args []Value, scope *Scope) Result

// Built-in verb dispatch table - all verbs are C-level helpers
var builtins = map[string]BuiltinFunc{
	// Core verbs
	"echo":   builtinEcho,
	"set":    builtinSet,
	"exit":   builtinExit,
	"return": builtinReturn,

	// File system verbs
	"cd":     builtinCd,
	"copy":   builtinCopy,
	"move":   builtinMove,
	"delete": builtinDelete,
	"mkdir":  builtinMkdir,
	"touch":  builtinTouch,
	"link":   builtinLink,
	"exists": builtinExists,

	// Utility verbs
	"len":   builtinLen,
	"glob":  builtinGlob,
	"match": builtinMatch,
	"hash":  builtinHash,
	"sleep": builtinSleep,

	// I/O verbs
	"env":    builtinEnv,
	"prompt": builtinPrompt,

	// Process verbs
	"run":   builtinRun,
	"spawn": builtinSpawn,
	"wait":  builtinWait,

	// Arithmetic verb
	"arith": builtinArith,

	// String manipulation verbs
	"join": builtinJoin,
	"cat":  builtinCat,

	// Network verbs (spec-compliant pure implementations)
	"download": builtinDownload,
	"untar":    builtinUntar,

	// Control flow helpers
	"test":     builtinTest,
	"break":    builtinBreak,
	"continue": builtinContinue,
}

var (
	spawnedProcs = make(map[int]*exec.Cmd)
	procMutex    sync.Mutex
)

// File system verbs implementation

func builtinCopy(args []Value, scope *Scope) Result {
	if len(args) != 2 {
		return Result{Error: &BoxError{Message: "copy: requires exactly two arguments (source, dest)"}}
	}

	src := args[0].String()
	dst := args[1].String()

	srcFile, err := os.Open(src)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("copy: %v", err)}}
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("copy: %v", err)}}
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("copy: %v", err)}}
	}

	return Result{Status: 0}
}

func builtinMove(args []Value, scope *Scope) Result {
	if len(args) != 2 {
		return Result{Error: &BoxError{Message: "move: requires exactly two arguments (source, dest)"}}
	}

	src := args[0].String()
	dst := args[1].String()

	err := os.Rename(src, dst)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("move: %v", err)}}
	}

	return Result{Status: 0}
}

func builtinDelete(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "delete: requires exactly one argument"}}
	}

	path := args[0].String()
	err := os.RemoveAll(path)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("delete: %v", err)}}
	}

	return Result{Status: 0}
}

func builtinMkdir(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "mkdir: requires exactly one argument"}}
	}

	path := args[0].String()
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("mkdir: %v", err)}}
	}

	return Result{Status: 0}
}

func builtinTouch(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "touch: requires exactly one argument"}}
	}

	path := args[0].String()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("touch: %v", err)}}
	}
	file.Close()

	return Result{Status: 0}
}

func builtinLink(args []Value, scope *Scope) Result {
	if len(args) != 2 {
		return Result{Error: &BoxError{Message: "link: requires exactly two arguments (target, link)"}}
	}

	target := args[0].String()
	link := args[1].String()

	err := os.Symlink(target, link)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("link: %v", err)}}
	}

	return Result{Status: 0}
}

// Utility verbs implementation

func builtinLen(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "len: requires exactly one argument"}}
	}

	value := args[0]
	length := len(value.List())
	lengthStr := strconv.Itoa(length)

	// Set result in a variable accessible to caller
	scope.Set("_len_result", Value{lengthStr})

	return Result{Status: 0}
}

func builtinGlob(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "glob: requires exactly one argument"}}
	}

	pattern := args[0].String()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("glob: %v", err)}}
	}

	// Set result in a variable accessible to caller
	scope.Set("_glob_result", Value(matches))

	return Result{Status: 0}
}

func builtinMatch(args []Value, scope *Scope) Result {
	if len(args) != 2 {
		return Result{Error: &BoxError{Message: "match: requires exactly two arguments (pattern, string)"}}
	}

	pattern := args[0].String()
	text := args[1].String()

	matched, err := filepath.Match(pattern, text)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("match: %v", err)}}
	}

	if matched {
		return Result{Status: 0}
	}
	return Result{Status: 1}
}

func builtinHash(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "hash: requires exactly one argument"}}
	}

	text := args[0].String()
	hash := sha256.Sum256([]byte(text))
	hashStr := hex.EncodeToString(hash[:])

	// Set result in a variable accessible to caller
	scope.Set("_hash_result", Value{hashStr})

	return Result{Status: 0}
}

func builtinSleep(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "sleep: requires exactly one argument"}}
	}

	durationStr := args[0].String()
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		// Try parsing as seconds
		if seconds, err2 := strconv.ParseFloat(durationStr, 64); err2 == nil {
			duration = time.Duration(seconds * float64(time.Second))
		} else {
			return Result{Error: &BoxError{Message: fmt.Sprintf("sleep: invalid duration: %v", err)}}
		}
	}

	time.Sleep(duration)
	return Result{Status: 0}
}

// I/O verbs implementation

func builtinEnv(args []Value, scope *Scope) Result {
	if len(args) == 0 {
		// List all environment variables
		environ := os.Environ()
		scope.Set("_env_result", Value(environ))
		return Result{Status: 0}
	}

	if len(args) == 1 {
		// Get specific environment variable
		key := args[0].String()
		value := os.Getenv(key)
		scope.Set("_env_result", Value{value})
		return Result{Status: 0}
	}

	if len(args) == 2 {
		// Set environment variable
		key := args[0].String()
		value := args[1].String()
		err := os.Setenv(key, value)
		if err != nil {
			return Result{Error: &BoxError{Message: fmt.Sprintf("env: %v", err)}}
		}
		return Result{Status: 0}
	}

	return Result{Error: &BoxError{Message: "env: requires 0, 1, or 2 arguments"}}
}

func builtinPrompt(args []Value, scope *Scope) Result {
	if len(args) > 1 {
		return Result{Error: &BoxError{Message: "prompt: requires 0 or 1 argument"}}
	}

	// Print prompt if provided
	if len(args) == 1 {
		fmt.Print(args[0].String())
	}

	// Read input
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := scanner.Text()
		scope.Set("_prompt_result", Value{input})
		return Result{Status: 0}
	}

	if err := scanner.Err(); err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("prompt: %v", err)}}
	}

	// EOF reached
	return Result{Status: 1}
}

// Network and archive verbs implementation (spec-compliant pure implementations)

func builtinDownload(args []Value, scope *Scope) Result {
	if len(args) < 2 {
		return Result{Error: &BoxError{Message: "download: requires at least two arguments (url, destination, [expected_hash])"}}
	}

	url := args[0].String()
	destination := args[1].String()
	var expectedHash string
	if len(args) > 2 {
		expectedHash = args[2].String()
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("download: failed to create directory: %v", err)}}
	}

	// Download file
	resp, err := http.Get(url)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("download: failed to fetch %s: %v", url, err)}}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{Error: &BoxError{Message: fmt.Sprintf("download: HTTP %d for %s", resp.StatusCode, url)}}
	}

	// Create output file
	outFile, err := os.Create(destination)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("download: failed to create %s: %v", destination, err)}}
	}
	defer outFile.Close()

	// Copy with hash verification if provided
	var hasher hash.Hash
	var writer io.Writer = outFile

	if expectedHash != "" {
		hasher = sha256.New()
		writer = io.MultiWriter(outFile, hasher)
	}

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("download: failed to write %s: %v", destination, err)}}
	}

	// Verify hash if provided
	if expectedHash != "" && hasher != nil {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if actualHash != expectedHash {
			os.Remove(destination) // Clean up on hash mismatch
			return Result{Error: &BoxError{
				Message: fmt.Sprintf("download: hash mismatch for %s", destination),
				Help:    fmt.Sprintf("Expected: %s, Got: %s", expectedHash, actualHash),
			}}
		}
	}

	return Result{Status: 0}
}

func builtinUntar(args []Value, scope *Scope) Result {
	if len(args) != 2 {
		return Result{Error: &BoxError{Message: "untar: requires exactly two arguments (archive, destination)"}}
	}

	// For now, return an error indicating this needs external implementation
	// A full tar implementation would be quite large for this context
	return Result{Error: &BoxError{
		Message: "untar: not implemented - would require full tar archive parsing",
		Help:    "Use external tar command or implement with archive/tar package",
	}}
}

// Arithmetic verb implementation

func builtinArith(args []Value, scope *Scope) Result {
	if len(args) != 3 {
		return Result{Error: &BoxError{Message: "arith: requires exactly three arguments (operand1, operator, operand2)"}}
	}

	op1Str := args[0].String()
	operator := args[1].String()
	op2Str := args[2].String()

	op1, err := strconv.ParseFloat(op1Str, 64)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("arith: invalid number: %s", op1Str)}}
	}

	op2, err := strconv.ParseFloat(op2Str, 64)
	if err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("arith: invalid number: %s", op2Str)}}
	}

	var result float64

	switch operator {
	case "+":
		result = op1 + op2
	case "-":
		result = op1 - op2
	case "*":
		result = op1 * op2
	case "/":
		if op2 == 0 {
			return Result{Error: &BoxError{Message: "arith: division by zero"}}
		}
		result = op1 / op2
	case "%":
		if op2 == 0 {
			return Result{Error: &BoxError{Message: "arith: modulo by zero"}}
		}
		result = math.Mod(op1, op2)
	case "**":
		result = math.Pow(op1, op2)
	default:
		return Result{Error: &BoxError{Message: fmt.Sprintf("arith: unknown operator: %s", operator)}}
	}

	// Store result as both integer and float representations
	resultStr := ""
	if result == math.Trunc(result) {
		resultStr = strconv.Itoa(int(result))
	} else {
		resultStr = strconv.FormatFloat(result, 'f', -1, 64)
	}

	scope.Set("_arith_result", Value{resultStr})

	return Result{Status: 0}
}

// String manipulation verbs implementation

func builtinJoin(args []Value, scope *Scope) Result {
	if len(args) < 2 {
		return Result{Error: &BoxError{Message: "join: requires at least two arguments (separator, value1, ...)"}}
	}

	separator := args[0].String()
	var values []string

	for _, arg := range args[1:] {
		values = append(values, arg.List()...)
	}

	result := strings.Join(values, separator)
	scope.Set("_join_result", Value{result})

	// Also output the result for command substitution and pipelines
	fmt.Print(result)

	return Result{Status: 0}
}

func builtinCat(args []Value, scope *Scope) Result {
	if len(args) > 0 {
		// If arguments provided, output them (like echo)
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Print(strings.Join(parts, " "))
	} else {
		// No arguments, read from stdin and output to stdout
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return Result{Error: &BoxError{Message: fmt.Sprintf("cat: %v", err)}}
		}
	}

	return Result{Status: 0}
}

// Core verbs implementation

func builtinSet(args []Value, scope *Scope) Result {
	if len(args) < 1 {
		return Result{Error: &BoxError{Message: "set: missing variable name"}}
	}

	varName := args[0].String()
	var values []string

	for _, arg := range args[1:] {
		values = append(values, arg.List()...)
	}

	scope.Set(varName, Value(values))
	return Result{Status: 0}
}

func builtinEcho(args []Value, scope *Scope) Result {
	var parts []string
	for _, arg := range args {
		parts = append(parts, arg.String())
	}

	fmt.Println(strings.Join(parts, " "))
	return Result{Status: 0}
}

func builtinExit(args []Value, scope *Scope) Result {
	status := 0
	if len(args) > 0 {
		if s, err := strconv.Atoi(args[0].String()); err == nil {
			status = s
		}
	}

	return Result{Status: status, Halt: true}
}

func builtinReturn(args []Value, scope *Scope) Result {
	status := 0
	if len(args) > 0 {
		if s, err := strconv.Atoi(args[0].String()); err == nil {
			status = s
		}
	}

	// Update status variable before returning
	scope.Set("status", Value{strconv.Itoa(status)})

	return Result{Status: status, Halt: true}
}

func builtinCd(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "cd: requires exactly one argument"}}
	}

	dir := args[0].String()
	if err := os.Chdir(dir); err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("cd: %v", err)}}
	}

	return Result{Status: 0}
}

// Additional built-ins for control flow
func builtinBreak(args []Value, scope *Scope) Result {
	return Result{Status: 0, Halt: true} // Special break signal
}

func builtinContinue(args []Value, scope *Scope) Result {
	return Result{Status: 0, Halt: true} // Special continue signal
}

// Built-ins for testing conditions
func builtinExists(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "exists: requires exactly one argument"}}
	}

	path := args[0].String()
	if _, err := os.Stat(path); err == nil {
		return Result{Status: 0}
	}

	return Result{Status: 1}
}

func builtinTest(args []Value, scope *Scope) Result {
	if len(args) == 0 {
		return Result{Status: 1}
	}

	// Simple test - non-empty string is true
	if args[0].String() != "" {
		return Result{Status: 0}
	}

	return Result{Status: 1}
}

// Process management verbs

func builtinRun(args []Value, scope *Scope) Result {
	if len(args) == 0 {
		return Result{Error: &BoxError{Message: "run: requires at least one argument (command)"}}
	}

	cmdName := args[0].String()
	var cmdArgs []string
	for _, a := range args[1:] {
		cmdArgs = append(cmdArgs, a.String())
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return Result{Status: exitErr.ExitCode()}
		}
		return Result{Error: &BoxError{Message: fmt.Sprintf("run: %v", err)}}
	}

	return Result{Status: 0}
}

func builtinSpawn(args []Value, scope *Scope) Result {
	if len(args) == 0 {
		return Result{Error: &BoxError{Message: "spawn: requires at least one argument (command)"}}
	}

	cmdName := args[0].String()
	var cmdArgs []string
	for _, a := range args[1:] {
		cmdArgs = append(cmdArgs, a.String())
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return Result{Error: &BoxError{Message: fmt.Sprintf("spawn: %v", err)}}
	}

	pid := cmd.Process.Pid
	procMutex.Lock()
	spawnedProcs[pid] = cmd
	procMutex.Unlock()

	return Result{Status: pid}
}

func builtinWait(args []Value, scope *Scope) Result {
	if len(args) != 1 {
		return Result{Error: &BoxError{Message: "wait: requires exactly one argument (PID)"}}
	}

	pid, err := strconv.Atoi(args[0].String())
	if err != nil {
		return Result{Error: &BoxError{Message: "wait: invalid PID"}}
	}

	procMutex.Lock()
	cmd, ok := spawnedProcs[pid]
	procMutex.Unlock()
	if !ok {
		return Result{Error: &BoxError{Message: fmt.Sprintf("wait: unknown pid %d", pid)}}
	}

	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			procMutex.Lock()
			delete(spawnedProcs, pid)
			procMutex.Unlock()
			return Result{Error: &BoxError{Message: fmt.Sprintf("wait: %v", err)}}
		}
	} else {
		exitCode = cmd.ProcessState.ExitCode()
	}

	procMutex.Lock()
	delete(spawnedProcs, pid)
	procMutex.Unlock()

	return Result{Status: exitCode}
}
