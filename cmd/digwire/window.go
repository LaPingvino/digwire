package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// getWindowsBrowserCandidates returns a prioritized list of browser executable paths on Windows
func getWindowsBrowserCandidates() []string {
	var list []string
	progFiles := os.Getenv("ProgramFiles")
	progFilesX86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")

	// Microsoft Edge (standard default on Windows 10/11)
	edgePaths := []string{
		filepath.Join(progFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(progFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),
	}
	// Google Chrome
	chromePaths := []string{
		filepath.Join(progFiles, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(progFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
	}
	// Brave Browser
	bravePaths := []string{
		filepath.Join(progFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(progFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}
	// Vivaldi
	vivaldiPaths := []string{
		filepath.Join(localAppData, "Vivaldi", "Application", "vivaldi.exe"),
		filepath.Join(progFiles, "Vivaldi", "Application", "vivaldi.exe"),
		filepath.Join(progFilesX86, "Vivaldi", "Application", "vivaldi.exe"),
	}

	allProbes := append(append(append(edgePaths, chromePaths...), bravePaths...), vivaldiPaths...)
	for _, p := range allProbes {
		if p != "" {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				list = append(list, p)
			}
		}
	}

	// Also check PATH
	for _, name := range []string{"msedge.exe", "msedge", "chrome.exe", "chrome", "brave.exe", "brave"} {
		if p, err := exec.LookPath(name); err == nil {
			list = append(list, p)
		}
	}
	return list
}

// launchNativeWindow opens the URL in a dedicated standalone app window or browser.
// Returns the running process command and whether it is a dedicated standalone app window
// (where closing the window should terminate the application daemon).
func launchNativeWindow(url string, noGTK bool, preferBrowser bool) (*exec.Cmd, bool) {
	var userDataDir string
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, _ := os.UserHomeDir()
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		userDataDir = filepath.Join(localAppData, "Digwire", "app-profile")
	} else {
		home, _ := os.UserHomeDir()
		userDataDir = filepath.Join(home, ".config", "digwire", "app-profile")
	}
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
			browserCmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
			if err := browserCmd.Start(); err == nil {
				return browserCmd, false
			}
			browserCmd = exec.Command("cmd", "/c", "start", "", url)
		}
		if browserCmd != nil {
			if err := browserCmd.Start(); err == nil {
				return browserCmd, false
			}
		}
	}

	// 1. Try native GTK3 WebKit2GTK window on Linux if not disabled
	if runtime.GOOS == "linux" && !noGTK {
		if runNativeGTKWindow(url) {
			p, _ := os.FindProcess(os.Getpid())
			_ = p.Signal(os.Interrupt)
			return nil, true
		}
	}

	// 2. Standalone Chromium/Chrome/Edge App Mode on Windows
	if runtime.GOOS == "windows" {
		candidates := getWindowsBrowserCandidates()
		for _, exePath := range candidates {
			cmd := exec.Command(
				exePath,
				fmt.Sprintf("--app=%s", url),
				fmt.Sprintf("--user-data-dir=%s", userDataDir),
				"--window-size=980,720",
				"--app-id=digwire",
			)
			if err := cmd.Start(); err == nil {
				return cmd, true
			}
		}

		// Windows System Browser Fallback (non-app window)
		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		if err := cmd.Start(); err == nil {
			return cmd, false
		}
		cmd2 := exec.Command("cmd", "/c", "start", "", url)
		if err := cmd2.Start(); err == nil {
			return cmd2, false
		}
		return nil, false
	}

	// 3. Standalone Chromium/Chrome/Edge App Mode or System Browser Fallback on Linux / macOS
	var candidates [][]string
	var isAppList []bool

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
		isAppList = []bool{true, true, true, true, true, true, false, false}
	case "darwin":
		candidates = [][]string{
			{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", fmt.Sprintf("--app=%s", url), fmt.Sprintf("--user-data-dir=%s", userDataDir), "--window-size=960,720"},
			{"open", url},
		}
		isAppList = []bool{true, true, true, false}
	}

	for i, cand := range candidates {
		binName := cand[0]
		path, err := exec.LookPath(binName)
		if err == nil || filepath.IsAbs(binName) {
			target := binName
			if path != "" {
				target = path
			}
			cmd := exec.Command(target, cand[1:]...)
			if err := cmd.Start(); err == nil {
				isApp := true
				if i < len(isAppList) {
					isApp = isAppList[i]
				}
				return cmd, isApp
			}
		}
	}

	return nil, false
}
