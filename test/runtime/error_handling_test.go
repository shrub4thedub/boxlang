package runtime

import (
	"testing"
	"box/test"
)

func TestErrorHandling(t *testing.T) {
	tests := []test.TestCase{
		{
			Name: "fail-fast default",
			Script: `[main]
echo "before"
exists "/nonexistent/path"
echo "after"
end`,
			ExitCode: 1,
			Stdout:   "before",
		},
		{
			Name: "ignore error with ?",
			Script: `[main]
echo "before"
exists "/nonexistent/path" ?
echo "after"
end`,
			ExitCode: 0,
			Stdout: `before
after`,
		},
		{
			Name: "fallback with ? command",
			Script: `[main]
echo "before"
exists "/nonexistent/path" ? echo "file not found"
echo "after"
end`,
			ExitCode: 0,
			Stdout: `before
file not found
after`,
		},
		{
			Name: "try-fallback-halt with !",
			Script: `[main]
echo "before"
exists "/nonexistent/path" ! echo "critical error"
echo "should not print"
end`,
			ExitCode: 1,
			Stdout: `before
critical error`,
		},
		{
			Name: "nested fallbacks",
			Script: `[main]
exists "/bad1" ? exists "/bad2" ? echo "double fallback"
echo "continued"
end`,
			ExitCode: 0,
			Stdout: `double fallback
continued`,
		},
		{
			Name: "arithmetic error handling",
			Script: `[main]
arith 10 "/" 0 ? echo "division by zero handled"
echo "program continues"
end`,
			ExitCode: 0,
			Stdout: `division by zero handled
program continues`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.Name, func(t *testing.T) {
			test.RunBoxTest(t, testCase)
		})
	}
}

func TestControlFlow(t *testing.T) {
	tests := []test.TestCase{
		{
			Name: "if statement true",
			Script: `[main]
if exists "."
  echo "current dir exists"
end
end`,
			ExitCode: 0,
			Stdout:   "current dir exists",
		},
		{
			Name: "if statement false",
			Script: `[main]
if exists "/nonexistent"
  echo "should not print"
else
  echo "in else block"
end
end`,
			ExitCode: 0,
			Stdout:   "in else block",
		},
		{
			Name: "for loop",
			Script: `[main]
for i in 1 2 3
  echo "item: $i"
end
end`,
			ExitCode: 0,
			Stdout: `item: 1
item: 2
item: 3`,
		},
		{
			Name: "function with return",
			Script: `[fn test_func]
echo "in function"
return 0
echo "should not print"
end

[main]
test_func
echo "after function"
end`,
			ExitCode: 0,
			Stdout: `in function
after function`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.Name, func(t *testing.T) {
			test.RunBoxTest(t, testCase)
		})
	}
}