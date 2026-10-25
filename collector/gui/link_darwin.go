//go:build darwin && desktop

package main

// Wails v2.12.0 macOS 프런트엔드가 UTType(UniformTypeIdentifiers)을 쓰지만 이 프레임워크를 링크하지
// 않아 최신 Xcode SDK에서 `_OBJC_CLASS_$_UTType` 미정의로 링크가 실패한다(#40 macOS 러너 실측).

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
