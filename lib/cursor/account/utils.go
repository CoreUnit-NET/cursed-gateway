package cursor_account_sdk

/*
Optional helpers (defaults, small shared values). Delete if unused.
*/

import (
	"fmt"
	"log/slog"
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

// PrintLoginURL logs the login URL and that authorization is pending (slog).
func PrintLoginURL(loginURL string) {
	slog.Info("open this URL in your browser to authorize Cursor", "url", loginURL)
	slog.Info("waiting for authorization")
}

// DefaultOnLoginURL logs the URL and best-effort opens a browser.
func DefaultOnLoginURL(loginURL string) error {
	PrintLoginURL(loginURL)
	if err := OpenBrowser(loginURL); err != nil {
		slog.Warn("failed to open browser", "err", err)
	}
	return nil
}

// IsExpired is a convenience around ExpiresAt unix-ms.
func IsExpired(expiresAtMilli int64, now time.Time) bool {
	if expiresAtMilli <= 0 {
		return true
	}
	return now.UnixMilli() >= expiresAtMilli
}
