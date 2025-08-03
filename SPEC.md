BOX - shell.
---

## 0 Philosophy at a glance

| Pillar                            | Concrete rule                                                                |
| --------------------------------- | ---------------------------------------------------------------------------- |
| Lists everywhere                  | Every variable is a **list of strings**; no implicit word-splitting.         |
| One postcard of grammar           | Only 11 token kinds; every block ends with `end`.                            |
| Fail-fast by default              | Any non-zero exit aborts the current scope unless the command ends with `?`. |
| Extensible by verbs, never syntax | New power arrives as **built-ins** written in C; the grammar never changes.  |


Think “Plan 9 rc with Make-like determinism and a ketogenic diet, written in golang”

---

## 1 Lexing & token rules

| Token                    | Example                                      | Notes                                                        |                                                    |
| ------------------------ | -------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------- |
| **word**                 | `build.o`                                    | Bytes except unescaped whitespace or special chars below.    |                                                    |
| **single-quote**         | `'raw bytes $no_expand'`                     | No interpolation at all.                                     |                                                    |
| **double-quote**         | `"C-style escapes \n"`                       | `\`-escapes, but variable/command substitution still occurs. |                                                    |
| **command substitution** | `` `uname -s` ``  `$(git rev-parse --short)` | Result is a **list** produced by the child command.          |                                                    |
| **variable forms**       | `$x`  `${x[*]}`  `${x[2]}`                   | First element, whole list, indexed element (0-based).        |                                                    |
| **header lookup**        | `${data.pkg.repo}`                           | Dot-path digs into `[data]` block.                           |                                                    |
| **redirections**         | `>` `>>` `2>`                                | Same semantics as POSIX shells.                              |                                                    |
| **pipeline**             | \`                                           | \`                                                           | Collects every child’s exit status into `$status`. |
| **ignore-error flag**    | `?`                                          | Suppresses fail-fast for that command only.                  |                                                    |
| **header start**         | `[fn build]`                                 | Also `[data pkg]`, `[main]`, etc.                            |                                                    |
| **block terminator**     | `end`                                        | The one and only.                                            |                                                    |
| **comment**              | `# every token after # is ignored to EOL`    | Whitespace not required before `#`.                          |                                                    |

> **No other punctuation is reserved.**
> Semicolons, braces `{}`, background `&`, and here-docs intentionally do **not** exist.

---

## 2 High-level file structure

```
[data <label>]
  key   value …   # arbitrary k-v pairs, list-valued
  other stuff
end

[fn <name> arg1 arg2=default …]
  …commands…
end

[main]
  …commands…
end
```

* If `[main]` is absent, top-level commands run directly.
* Arguments in `[fn]` headers may carry simple defaults (`dir=/tmp`).
* Headers may nest arbitrarily, but **only `fn`, `data`, and `main` have interpreter meaning**; others are user metadata.

### Importing other files

Use `import path/to/file.box` to pull in code from another file. Box derives a
namespace from the filename—`util.box` becomes `util`. Functions and data inside
that file are then accessible as `util.fnname` or `util.block.field`. The
imported file's `[main]` block is ignored. Re-importing the same path is a
no-op, while namespace clashes raise an error. Only `.box` files are accepted.

### 2.1 Block Modifiers

Block headers support **modifiers** that alter their behavior:

```box
[data -c config]         # -c: constant data
  name    box-shell
  version 0.1.0
end

[fn -i greet name]       # -i: invokable from CLI
  echo Hello $name
end

[fn -h helper]           # -h: hidden/internal
  echo This is private
end
```

#### Available Modifiers

| Modifier | Applies to | Purpose | Example |
|----------|------------|---------|---------|
| **`-c`** | `[data]` | **Constant**: Marks data as immutable/static | `[data -c config]` |
| **`-i`** | `[fn]` | **Invokable**: Enable CLI dispatch for function | `[fn -i build target]` |
| **`-h`** | `[fn]`, `[data]` | **Hidden**: Mark as internal/auxiliary | `[fn -h helper]` |

#### CLI Dispatch with `-i`

Functions marked with `-i` can be invoked directly from the command line:

```bash
# Instead of running [main], call the function directly
./script.box greet Alice        # calls greet("Alice") 
./script.box build debug        # calls build("debug")
./script.box configure key val  # calls configure("key", "val")
```

**Rules:**
- If a matching `-i` function exists, it runs instead of `[main]`
- Function parameters are passed as command-line arguments
- If no match found, `[main]` executes normally
- Multiple `-i` functions per file are supported

#### Constant Data with `-c`

Data blocks marked with `-c` declare their content as constant/immutable:

```box
[data -c build_info]
  version 1.2.3
  target  release
end

[main]
  echo Building ${build_info[0]} version ${build_info[1]}
end
```

#### Hidden Blocks with `-h` 

Functions and data marked with `-h` are internal/auxiliary:

```box
[fn -h validate input]
  # Internal validation logic
  if test -z $input
    return 1
  end
end

[fn -i process file]
  if not validate $file
    echo "Invalid file: $file"
    exit 1
  end
  # Process the file...
end
```

**Note:** Hidden functions can still be called from within the same file.

---

## 3 Variables & the `set` verb

```box
set files  a.c  "b space.c"
```

* `set` assigns “everything after the verb” to **one list**.
* Variables are **lexical**: a child function inherits its caller’s bindings by reference; re-`set` shadows.

Retrieval:

| Form          | Result                                          |
| ------------- | ----------------------------------------------- |
| `$files`      | first element (`a.c`)                           |
| `${files[*]}` | whole list, space-joined when coerced to string |
| `${files[1]}` | second element (`b space.c`)                    |

Lists never auto-expand—**no `$IFS` equivalents exist.**

---

## 4 Commands, pipelines, and error policy

### 4.1 One physical line ⇒ one command

No semicolons; readability wins.

### 4.2 Fail-fast rules

* Non-zero exit **aborts current scope** (`fn`, `[main]`, or file).
* Add `?` to ignore exit code:

  ```box
  move build ?        # tolerate “already exists”
  run ./configure ? | tee config.log
  ```

### 4.3 Pipelines

`cmd1 | cmd2 | cmd3` runs in parallel; when finished:

```
$status = (exit1 exit2 exit3)
```

Each element is a **decimal integer** (signals/cores converted).

---

## 5 Control flow

```box
if exists foo.c
  echo found
else
  echo missing
end

for f in glob src/*.c
  run cc -c $f -o ${f%.c}.o
end

while arith $i < 10
  i = $(arith $i + 1)
end

break        # leave nearest for/while
continue     # next loop iteration
return 0     # from within a function
exit 42      # terminate whole script
```

Blocks always close with `end`.

## 6 Built-in verbs (core)

> Alphabetical; verbs marked **🆕** were added in this revision.

| Verb             | Signature (*italic* = optional) | Purpose / semantics                                                        |
| ---------------- | ------------------------------- | -------------------------------------------------------------------------- |
| **arith**        | `arith EXPR…`                   | Evaluate integer expression (supports `+ - * / % == != < > <= >=`).        |
| **cd**           | `cd DIR`                        | Change directory (fail-fast).                                              |
| **copy**         | `copy SRC… to DST/`             | Recursive copy; parents auto-created.                                      |
| **delete**       | `delete PATH…`                  | Remove files/dirs recursively (like `rm -rf`).                             |
| **echo**         | `echo ARG…`                     | Print list collapsed by spaces + newline.                                  |
| **env**          | `env KEY [VALUE]`               | Read variable (1 arg) or set (2 args).                                     |
| **exists**       | `exists PATH`                   | Exit 0 if path exists else 1.                                              |
| **exit**         | `exit *STATUS*`                 | Terminate script immediately.                                              |
| **glob**         | `glob PAT…`                     | Expand shell globs; returns list.                                          |
| **hash**         | `hash alg file…`                | Print `<sum> file` lines (SHA-256 etc.).                                   |
| **len**          | `len LIST`                      | Output list length.                                                        |
| **link**         | `link SRC… to DST/`             | Hard-link files.                                                           |
| **match** **🆕** | `match ITEM PAT…`               | Exit 0 if *ITEM* matches any shell-style *PAT*; else 1.<br>`match foo *.c` |
| **mkdir**        | `mkdir DIR…`                    | `mkdir -p` behaviour; idempotent.                                          |
| **move**         | `move SRC… to DST/`             | Rename/move; atomic on same file-system.                                   |
| **prompt**       | `prompt MSG…`                   | Print message, read one line into `$reply`.                                |
| **return**       | `return *STATUS*`               | Exit current function.                                                     |
| **run**          | `run CMD ARG…`                  | Fork/exec external program, propagate status.                              |
| **sleep**        | `sleep SECONDS`                 | Suspend (fractional allowed).                                              |
| **spawn** **🆕** | `spawn CMD ARG…`                | Fork/exec **in background**, return child PID in `$status`.                |
| **wait** **🆕**  | `wait PID`                      | Block until PID exits; put exit code in `$status`.                         |
| **touch**        | `touch FILE…`                   | Create or update timestamp.                                                |

All verbs are **pure C helpers**—no `system(3)` shell outs.

---

## 7 Worked examples

### 7.1 Content-addressed fetch & build

```box
[data src]
  url   https://example.com/app.tgz
  sha   9c1185a5c5e9fc...           # SHA-256
end

[main]
  mkdir cache ?
  download ${src.url} cache/app.tgz ${src.sha}
  untar cache/app.tgz build/
  run cc build/*.c -o app
end
```

Uses verbs (`download`, `untar`, `hash`) that satisfy the “small-pure” rule.

---

### 7.2 Parallel test runner with `spawn` / `wait`

```box
set pids
for t in glob tests/*.box
  spawn box $t
  set pids ${pids[*]} $status       # append PID
end

for pid in ${pids[*]}
  wait $pid ?                       # ignore individual failures
end

if match ${status[*]} *[1-9]*
  echo "some tests failed" ; exit 1
end
```

* **`spawn`** runs each test in the background and yields its PID.
* **`wait`** reaps them; `?` lets the loop continue even if a test fails.
* Final `match` checks if any exit code was non-zero.

---

### 7.3 Pattern-directed compilation

```box
for f in glob src/*
  if match $f *.c
    run cc -c $f -o ${f%.c}.o
  else if match $f *.s
    run as     $f -o ${f%.s}.o
  end
end
```

`match` eliminates ad-hoc string slicing.

---

### 7.4 List tricks

```box
set nums  1 2 3 4 5
echo len = $(len ${nums[*]})              # 5
echo 3rd = ${nums[2]}                     # 3
join , ${nums[*]} | echo csv = $status    # "1,2,3,4,5"
```

---

## 8 Design invariants to protect

1. **No new punctuation** unless you delete an existing one.
2. **Every addition is a verb** taking **lists in, lists out**, never raw strings.
3. **written in golang, full interperter under 2000 LoC altogether**

By holding those lines, Box scripts stay legible on a single screen, the binary stays embeddable in init-ramfs, and the audit surface remains the size of a novella rather than a novel.

---

## 9 Enhanced Error Handling Syntax

### 9.1 Error Suppression Operators

BOX provides three distinct error handling patterns:

#### `cmd ?` - Silent Error Suppression
Allow command to fail without aborting the current scope:
```box
mkdir temp ?                    # ignore "directory exists" 
git pull ?                      # continue if network fails
run ./configure ? | tee log     # capture output even on failure
```

#### `cmd ? fallback` - Error Suppression with Fallback
Execute fallback command on failure, then continue:
```box
run make ? echo "Build failed, using cached binary"
copy config.default config ? echo "Using default config"
download ${url} file.tgz ? echo "Download failed, check network"
```

#### `cmd ! fallback` - Try-Fallback-Halt
Attempt command, run fallback on failure, then halt execution:
```box
run ./tests ! echo "Tests failed - aborting" && exit 1
hash sha256 ${file} ! echo "File corrupt" && return 1
```

### 9.2 Error Handling Precedence
1. **No suffix**: Fail-fast (abort scope on non-zero exit)
2. **`?`**: Suppress errors, continue execution  
3. **`? fallback`**: Suppress errors, run fallback, continue
4. **`! fallback`**: Run fallback on failure, then halt

---

## 10 Beautiful Error Messages

BOX implements NuShell-inspired error formatting for clear, actionable feedback:

### 10.1 Command Not Found
```
✗ Command not found
  ╭─[build.box:12:1]
  │
12│   run gcc-missing -o app src/*.c
  │       ─────┬─────
  │            ╰── command 'gcc-missing' not found in PATH
  │
  │ Help: Did you mean 'gcc'?
```

### 10.2 File System Errors
```
✗ Permission denied
  ╭─[deploy.box:8:1]  
  │
 8│   copy app.bin /usr/local/bin/
  │                ──────┬─────── 
  │                      ╰── cannot write to '/usr/local/bin/' (permission denied)
  │
  │ Help: Try running with elevated privileges or choose a different destination
```

### 10.3 Type/Syntax Errors
```
✗ Invalid syntax
  ╭─[script.box:15:1]
  │
15│   if exists $file && test -x $file
  │                   ─┬─
  │                    ╰── unexpected '&&' - use separate 'if' blocks instead
  │
  │ Help: BOX doesn't support shell operators like '&&' or '||'
```

### 10.4 Runtime Errors with Context
```
✗ Pipeline failed
  ╭─[build.box:22:1]
  │
22│   run make -j8 | tee build.log | grep ERROR
  │       ────┬────
  │           ╰── command failed with exit code 2
  │
  │ Pipeline status: [2, 0, 1]
  │ Help: Check build.log for compilation errors
```

### 10.5 Error Message Design Principles
- **Precise location**: Line and column numbers with context
- **Visual clarity**: Unicode box-drawing for clean layouts
- **Actionable help**: Specific suggestions for resolution
- **Color coding**: Red for errors, yellow for warnings, blue for info
- **Context preservation**: Show surrounding code for debugging

---

## 11 Error Reporting System

BOX reports errors through a structured `BoxError` value so every failure carries its source location.

### 11.1 `BoxError` structure
- **Message**: human‑readable description of what went wrong
- **Location**: file, line and column of the offending code
- **Help**: optional hint for recovery
- **Code**: snippet that triggered the error

Parsing and runtime components must return a `BoxError` or wrap their internal errors into one. This ensures callers always receive location metadata.

### 11.2 CLI output
The CLI prints these errors using `FormatError`, which adds line context, colors and help text.

Every built‑in verb and parser failure must either produce a `BoxError` directly or propagate one unchanged so location is preserved throughout the stack.
