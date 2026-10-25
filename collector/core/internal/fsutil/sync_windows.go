//go:build windows

package fsutil

// Windows의 Chmod는 읽기 전용 비트만 다룬다. 파일은 사용자 프로필 디렉터리 ACL로 보호된다.
const chmodUnsupported = true

func syncDir(string) {}
