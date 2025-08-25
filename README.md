# boxlang
your new least favorite scripting language

boxlang is free software, provided to you at no charge under the terms of the
gnu general public license, version 3 or (at your option) any later version.
this software is provided without any warranty.

copyright © 2025 shrub industries

box is a simple scripting language inspired by plan9's rc shell and attempts to have deterministic behavior. it was created for use with the [pack package manager](https://github.com/shrub4thedub/pack), but can probably be used for other things if you're feeling adventurous. it's currently unfinished and likely wont do what you want it too.

box features lists everywhere, fail-fast by default, and simple small grammar that i, and by extension you, can remember. no semicolons, no braces, no bullshit.

## install
box is easy and fun to install through pack:

```bash
# if you have pack
pack open boxlang

# if you don't have pack yet
git clone https://github.com/shrub4thedub/pack.git
cd pack
./pack open pack
#(this installs box too)
```

or build from source if you're into that:

```bash
git clone https://github.com/shrub4thedub/boxlang.git
cd boxlang
go build -o box cmd/box/main.go
```

## how it works

box scripts have a simple structure: data blocks for configuration, function blocks for reusable code, and a main block that runs when you execute the script.

```box
# -c block modifier denotes 'constant'
[data -c config]
  name     myproject
  version  1.0
  author   me
end

[fn greet name]
  echo "hello, ${name}!"
  echo "welcome to ${data.config.name} v${data.config.version}"
end

[main]
  greet world
  greet "box user"
end
```

every variable is a list of strings, no word splitting rubbish. if a command fails, the whole script stops unless you add `?` to ignore errors.

```bash
# run a script
box myscript.box

# debug lexer output  
box lex myscript.box

# debug parser ast
box ast myscript.box

# interactive mode
box
```

## the language

### data blocks
store configuration and structured data:

```box
[data -c pkg]
  name     pack
  version  0.1
  deps     go box git
end

[data user]
  name     shrub
  email    shrub@shrub.industries
  groups   cool-guy
end
```

access with `${data.pkg.name}` or `${data.user.groups}` (gets whole list).

### functions
reusable code blocks with parameters:

```box
[fn build target]
  echo "building ${target}..."
  run gcc -o ${target} ${target}.c
  if exists ${target}
    echo "✓ ${target} built successfully"
  else
    echo "✗ build failed"
    exit 1
  end
end

[fn install bin dest]
  echo "installing ${bin} to ${dest}..."
  run cp ${bin} ${dest}
  run chmod +x ${dest}/${bin}
end
```

### built-in verbs
box comes with essential built-ins:

- `echo` - print text
- `set` - assign variables  
- `run` - execute external commands
- `cd` - change directory
- `env` - get environment variables
- `glob` - file pattern matching
- `exists` - check if files exist
- `delete` - remove files/directories
- `mkdir` - create directories
- `if/else/end` - conditionals
- `while/end` - loops
- `for/end` - iteration
- `import` - load other scripts
- `exit` - terminate with status

### control structures

```box
# conditionals
if exists config.txt
  echo "config found"
else
  echo "no config, creating default"
  echo "default=true" > config.txt
end

# loops
set files `ls *.c`
for file in ${files}
  echo "compiling ${file}..."
  run gcc -c ${file}
end

# while loops  
set count 1
while test ${count} -le 5
  echo "iteration ${count}"
  set count `expr ${count} + 1`
end
```

### error handling
commands fail fast by default. add `?` to ignore errors:

```box
# this will stop the script if ls fails
run ls /nonexistent

# this will continue even if ls fails  
run ls /nonexistent ?

# check exit status
run ls /maybe-exists ?
if test ${status} -eq 0
  echo "directory exists"
else
  echo "directory not found"
end
```

### lists and variables
everything is a list. access with `$var` (first element) or `${var}` (whole list):

```box
set fruits apple orange banana
echo ${fruits}        # prints: apple orange banana
echo ${fruits[0]}     # prints: apple  
echo ${fruits[2]}     # prints: banana
echo ${fruits[*]}     # prints: apple orange banana

# command substitution creates lists
set files `ls *.txt`
for file in ${files}
  echo "processing ${file}"
end
```

## examples

check out `examples/` for scripts that do stuff:

- `ed.box` - text editor functionality

## writing good box

- keep functions small and focused, and reusable
- add `?` only when you actually want to ignore errors
- organize data in logical blocks
- test with `box ast` to check parsing
- when it doesent work just use bash instead i wont blame you
- dont do anything complicated because this shit SUCKS. dont say i didnt warn you.

