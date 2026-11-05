package adapter

import (
	"encoding/json"
	"testing"
)

func shellInput(t *testing.T, command any) Object {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"command": command, "description": "x"})
	if err != nil {
		t.Fatal(err)
	}
	obj, err := DecodeObject(raw, MaxHookPayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestShellActivityTesting(t *testing.T) {
	for _, command := range []string{
		"pytest -q",
		"python3 -m pytest tests/test_x.py",
		"python -m unittest discover",
		"go test ./...",
		"cargo test --release",
		"npm test",
		"npm run -s test:unit",
		"npm t",
		"yarn test",
		"pnpm run test",
		"bun test",
		"npx vitest run",
		"cd apps/web && npx vitest run 2>&1 | tail -20",
		"CI=1 AGENT=x pytest",
		"sudo -n env FOO=1 go test ./internal/...",
		"uv run pytest",
		"poetry run pytest -x",
		"bundle exec rspec",
		"./node_modules/.bin/jest --watch=false",
		"npx playwright test",
		"make test",
		"./gradlew test",
		"dotnet test",
		"deno test -A",
		"(cd web; vitest)",
		"set -e\nnpm ci\nnpm test",
		`bash -lc "go test ./..."`,
		"pwsh -Command npm test",
	} {
		if got := ShellActivity(shellInput(t, command)); got != ActivityTesting {
			t.Errorf("%q: %q, want testing", command, got)
		}
	}
	if got := ShellActivity(shellInput(t, []string{"bash", "-lc", "go test ./..."})); got != ActivityTesting {
		t.Errorf("argv form: %q", got)
	}
}

func TestShellActivityNotTesting(t *testing.T) {
	for _, command := range []string{
		"ls -la",
		"git commit -m 'fix pytest'",
		"echo pytest",
		"grep -rn vitest src",
		"go build ./...",
		"go vet ./...",
		"npm run build",
		"npm install",
		"cargo build",
		"make",
		"make install",
		"python3 script.py test",
		"cat test.txt",
		"docker build -t test .",
		"",
	} {
		if got := ShellActivity(shellInput(t, command)); got != ActivityUnknown {
			t.Errorf("%q: %q, want unknown", command, got)
		}
	}
}

func TestShellActivityIgnoresOddInput(t *testing.T) {
	if got := ShellActivity(Object{}); got != ActivityUnknown {
		t.Errorf("missing command: %q", got)
	}
	if got := ShellActivity(shellInput(t, 42)); got != ActivityUnknown {
		t.Errorf("number command: %q", got)
	}
	long := make([]byte, MaxCommandBytes+1)
	for i := range long {
		long[i] = ' '
	}
	copy(long, "pytest")
	if got := ShellActivity(shellInput(t, string(long))); got != ActivityUnknown {
		t.Errorf("oversized command: %q", got)
	}
}
