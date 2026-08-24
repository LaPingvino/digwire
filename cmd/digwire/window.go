package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// launchNativeWindow opens the URL in a dedicated standalone app window (chromeless, native window frame)
func launchNativeWindow(url string) *exec.Cmd {
	home, _ := os.UserHomeDir()
	userDataDir := filepath.Join(home, ".config", "digwire", "app-profile")
	_ = os.MkdirAll(userDataDir, 0755)

	var candidates [][]string

	switch runtime.GOOS {
	case "linux":
		candidates = [][]string{
			{"google-chrome-stable", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"google-chrome", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"chromium", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"brave-browser", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"firefox", "--new-window", url},
			{"xdg-open", url},
		}
	case "darwin":
		candidates = [][]string{
			{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"open", url},
		}
	case "windows":
		candidates = [][]string{
			{"cmd", "/c", "start", "chrome", fmt.Sprintf("--app=%s", url), "--window-size=960,720"},
			{"cmd", "/c", "start", "msedge", fmt.Sprintf("--app=%s", url), "--window-size=960,720"},
			{"cmd", "/c", "start", url},
		}
	}

	for _, cand := range candidates {
		binName := cand[0]
		path, err := exec.LookPath(binName)
		if err == nil || filepath.IsAbs(binName) {
			target := binName
			if path != "" {
				target = path
			}
			cmd := exec.Command(target, cand[1:]...)
			if err := cmd.Start(); err == nil {
				return cmd
			}
		}
	}

	return nil
}
