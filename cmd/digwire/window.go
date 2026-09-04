package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// launchNativeWindow opens the URL in a dedicated standalone app window or browser
func launchNativeWindow(url string, noGTK bool, preferBrowser bool) *exec.Cmd {
	home, _ := os.UserHomeDir()
	userDataDir := filepath.Join(home, ".config", "digwire", "app-profile")
	_ = os.MkdirAll(userDataDir, 0755)

	// If preferBrowser is requested, directly launch default browser via standard OS opener
	if preferBrowser {
		var browserCmd *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			browserCmd = exec.Command("xdg-open", url)
		case "darwin":
			browserCmd = exec.Command("open", url)
		case "windows":
			browserCmd = exec.Command("cmd", "/c", "start", url)
		}
		if browserCmd != nil {
			if err := browserCmd.Start(); err == nil {
				return browserCmd
			}
		}
	}

	// 1. Try native GTK3 WebKit2GTK window on Linux if not disabled
	if runtime.GOOS == "linux" && !noGTK {
		if runNativeGTKWindow(url) {
			p, _ := os.FindProcess(os.Getpid())
			_ = p.Signal(os.Interrupt)
			return nil
		}
	}

	// 2. Standalone Chromium/Chrome/Edge App Mode or System Browser Fallback
	var candidates [][]string

	switch runtime.GOOS {
	case "linux":
		candidates = [][]string{
			{"google-chrome-stable", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"google-chrome", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"chromium", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"chromium-browser", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"brave-browser", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720", "--class=digwire", "--name=digwire", "--app-id=digwire"},
			{"microsoft-edge", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=980,720"},
			{"firefox", "--new-window", url},
			{"xdg-open", url},
		}
	case "darwin":
		candidates = [][]string{
			{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"open", url},
		}
	case "windows":
		candidates = [][]string{
			{"cmd", "/c", "start", "msedge", fmt.Sprintf("--app=%s", url), "--window-size=960,720"},
			{"cmd", "/c", "start", "chrome", fmt.Sprintf("--app=%s", url), "--window-size=960,720"},
			{"cmd", "/c", "start", "brave", fmt.Sprintf("--app=%s", url), "--window-size=960,720"},
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
