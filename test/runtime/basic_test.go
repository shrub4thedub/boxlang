package runtime

import (
	"testing"
	"box/test"
)

func TestBasicCommands(t *testing.T) {
	tests := []test.TestCase{
		{
			Name: "echo command",
			Script: `[main]
echo "hello world"
end`,
			ExitCode: 0,
			Stdout:   "hello world",
		},
		{
			Name: "variable assignment and access",
			Script: `[main]
set message "Hello Box"
echo $message
end`,
			ExitCode: 0,
			Stdout:   "Hello Box",
		},
		{
			Name: "data block access",
			Script: `[data config]
name "test-app"
version "1.0"
end

[main]
echo "App: ${config.name} v${config.version}"
end`,
			ExitCode: 0,
			Stdout:   "App: test-app v1.0",
		},
		{
			Name: "function definition and call",
			Script: `[fn greet name]
echo "Hello, $name!"
end

[main]
greet "Alice"
end`,
			ExitCode: 0,
			Stdout:   "Hello, Alice!",
		},
		{
			Name: "arithmetic operations",
			Script: `[main]
arith 5 "+" 3
echo "5 + 3 = $_arith_result"
arith 10 "*" 2
echo "10 * 2 = $_arith_result"
end`,
			ExitCode: 0,
			Stdout: `5 + 3 = 8
10 * 2 = 20`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.Name, func(t *testing.T) {
			test.RunBoxTest(t, testCase)
		})
	}
}

func TestBuiltinCommands(t *testing.T) {
	tests := []test.TestCase{
		{
			Name: "file operations",
			Script: `[main]
touch "test.txt"
exists "test.txt"
echo "File exists status: $status"
delete "test.txt"
end`,
			ExitCode: 0,
			Stdout:   "File exists status: 0",
		},
		{
			Name: "directory operations",
			Script: `[main]
mkdir "testdir"
exists "testdir"
echo "Directory created: $status"
delete "testdir"
end`,
			ExitCode: 0,
			Stdout:   "Directory created: 0",
		},
		{
			Name: "string length",
			Script: `[main]
set text "hello world"
len ${text[*]}
echo "Length: $_len_result"
end`,
			ExitCode: 0,
			Stdout:   "Length: 1",
		},
		{
			Name: "hash function",
			Script: `[main]
hash "test"
echo "Hash length check"
len $_hash_result
echo "Hash result length: $_len_result"
end`,
			ExitCode: 0,
			Stdout: `Hash length check
Hash result length: 1`,
		},
		{
			Name: "environment variables",
			Script: `[main]
env "TEST_VAR" "test_value"
env "TEST_VAR"
echo "Env var: $_env_result"
end`,
			ExitCode: 0,
			Stdout:   "Env var: test_value",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.Name, func(t *testing.T) {
			test.RunBoxTest(t, testCase)
		})
	}
}