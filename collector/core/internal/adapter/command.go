package adapter

import (
	"encoding/json"
	"path"
	"strings"
)

// 셸 명령 활동 분류(#280). 명령 원문은 이 함수 안에서만 읽고 버린다. 후보에는 activity enum만 남고
// 원문·실행 파일 이름·인자는 디스크·서버·로그 어디에도 쓰지 않는다.

// MaxCommandBytes보다 긴 명령은 분류하지 않는다(unknown).
const MaxCommandBytes = 16 << 10

// 실행 파일 이름만으로 테스트인 도구.
var testRunners = map[string]bool{
	"pytest": true, "py.test": true, "vitest": true, "jest": true, "mocha": true, "ava": true,
	"tox": true, "nox": true, "phpunit": true, "rspec": true, "ctest": true, "karma": true,
}

// `<도구> test`처럼 하위 명령이 test일 때 테스트인 도구.
var testSubcommands = map[string]bool{
	"go": true, "cargo": true, "npm": true, "yarn": true, "pnpm": true, "bun": true, "deno": true,
	"dotnet": true, "mvn": true, "gradle": true, "gradlew": true, "mix": true, "swift": true,
	"flutter": true, "dart": true, "make": true, "rake": true, "playwright": true,
}

// 앞에 붙어 실제 명령을 감싸는 도구. 건너뛰고 다음 단어를 본다.
var wrappers = map[string]bool{
	"sudo": true, "env": true, "time": true, "nice": true, "nohup": true, "exec": true, "command": true,
	"npx": true, "bunx": true, "pnpx": true, "xvfb-run": true,
}

var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "pwsh": true, "powershell": true, "cmd": true}

func shellFlag(arg string) bool {
	return arg == "-c" || arg == "-lc" || arg == "-ic" || strings.EqualFold(arg, "-Command") || strings.EqualFold(arg, "/c")
}

// ShellActivity는 셸 도구 입력의 command(문자열 또는 문자열 배열)를 보고 테스트 실행이면
// ActivityTesting, 아니면 ActivityUnknown을 돌려준다. `&&`·`;`·`|`로 이어진 명령 중 하나라도
// 테스트면 테스트다(`cd web && npx vitest run`).
func ShellActivity(input Object) Activity {
	command := input.String("command")
	if command == "" {
		var parts []string
		if raw, ok := input["command"]; ok && json.Unmarshal(raw, &parts) == nil {
			// Codex 배열 형식(`["bash", "-lc", "<명령>"]`)은 셸에 넘긴 마지막 인자가 실제 명령이다.
			if n := len(parts); n >= 3 && shellFlag(parts[n-2]) {
				parts = parts[n-1:]
			}
			command = strings.Join(parts, " ")
		}
	}
	if command == "" || len(command) > MaxCommandBytes {
		return ActivityUnknown
	}
	segments := strings.FieldsFunc(command, func(r rune) bool {
		return r == '&' || r == '|' || r == ';' || r == '\n' || r == '(' || r == ')'
	})
	for _, segment := range segments {
		if isTestCommand(strings.Fields(segment)) {
			return ActivityTesting
		}
	}
	return ActivityUnknown
}

func isTestCommand(words []string) bool {
	// 환경 변수 대입(`CI=1 pytest`)·감싸는 도구·`uv run`·`poetry run`·`bundle exec`를 건너뛴다.
	for len(words) > 0 {
		head := path.Base(words[0])
		switch {
		case strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-"):
			words = words[1:]
		case wrappers[head]:
			words = words[1:]
		case (head == "uv" || head == "poetry" || head == "pipenv" || head == "hatch") && len(words) > 1 && words[1] == "run",
			head == "bundle" && len(words) > 1 && words[1] == "exec":
			words = words[2:]
		case shells[head] && len(words) > 2 && shellFlag(words[1]):
			// `bash -lc "go test ./..."`: 따옴표를 벗기고 셸에 넘긴 명령을 본다.
			words = append([]string{strings.TrimLeft(words[2], `"'`)}, words[3:]...)
		case strings.HasPrefix(words[0], "-"):
			// sudo·env 옵션(`sudo -n`).
			words = words[1:]
		default:
			return testHead(head, words[1:])
		}
	}
	return false
}

func testHead(head string, args []string) bool {
	head = strings.TrimSuffix(strings.TrimSuffix(head, ".exe"), ".cmd")
	if testRunners[head] {
		return true
	}
	if strings.HasPrefix(head, "python") && len(args) > 1 && args[0] == "-m" {
		return args[1] == "pytest" || args[1] == "unittest"
	}
	if !testSubcommands[head] {
		return false
	}
	sub := firstPlain(args)
	if sub == "t" && (head == "npm" || head == "yarn" || head == "pnpm") {
		return true
	}
	if sub == "run" && (head == "npm" || head == "yarn" || head == "pnpm" || head == "bun") {
		sub = firstPlain(args[indexOf(args, "run")+1:])
		return strings.HasPrefix(sub, "test")
	}
	if head == "make" || head == "yarn" {
		return sub == "test" || sub == "check" || strings.HasPrefix(sub, "test")
	}
	return sub == "test"
}

// firstPlain은 옵션(`-s`, `--silent`)이 아닌 첫 단어다.
func firstPlain(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

func indexOf(args []string, value string) int {
	for i, arg := range args {
		if arg == value {
			return i
		}
	}
	return len(args) - 1
}
