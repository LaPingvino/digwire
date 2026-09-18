// Package appdir locates Digwire's own directory inside the user's configuration directory.
package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once sync.Once
	// ambientXDG is XDG_CONFIG_HOME as the process inherited it. A test that wants a specific
	// directory sets it afterwards; anything else is the user's own configuration, which a test
	// run must not touch even though it is pointed at by the environment.
	ambientXDG = os.Getenv("XDG_CONFIG_HOME")
	testRoot   string
)

// Dir is where Digwire keeps its session, its cached metadata and its DHT index.
//
// Under `go test` it is a throwaway directory unless the test points XDG_CONFIG_HOME somewhere
// itself: a test run must never touch the real one, where it would overwrite the session of the
// app in use.
func Dir() string {
	if root := testDir(); root != "" {
		return root
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	return filepath.Join(configDir, "digwire")
}

func testDir() string {
	if !underTest() || os.Getenv("XDG_CONFIG_HOME") != ambientXDG {
		return "" // The test chose a directory of its own.
	}
	once.Do(func() {
		dir, err := os.MkdirTemp("", "digwire-test-config-")
		if err == nil {
			testRoot = dir
		}
	})
	return testRoot
}

func underTest() bool {
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.") || strings.HasPrefix(arg, "--test.") {
			return true
		}
	}
	return false
}
