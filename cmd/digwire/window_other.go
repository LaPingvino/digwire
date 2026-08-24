//go:build !linux || !cgo
// +build !linux,!cgo

package main

func runNativeGTKWindow(url string) bool {
	return false
}
