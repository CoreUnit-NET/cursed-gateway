package cursor_account_sdk

/*
Optional helpers (defaults, small shared values). Delete if unused.
*/

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// OpenBrowser tries to open url with the platform default handler.
// Returns nil even if the open command fails after best-effort start
// when allowFail is true; otherwise returns the exec error.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// PrintLoginURL writes the login URL instructions to w (defaults to stderr).
func PrintLoginURL(url string) {
	fmt.Fprintf(os.Stderr, "Open this URL in your browser to authorize Cursor:\n\n  %s\n\nWaiting for authorization…\n", url)
}

// DefaultOnLoginURL prints the URL and best-effort opens a browser.
func DefaultOnLoginURL(loginURL string) error {
	PrintLoginURL(loginURL)
	_ = OpenBrowser(loginURL)
	return nil
}

// IsExpired is a convenience around ExpiresAt unix-ms.
func IsExpired(expiresAtMilli int64, now time.Time) bool {
	if expiresAtMilli <= 0 {
		return true
	}
	return now.UnixMilli() >= expiresAtMilli
}
