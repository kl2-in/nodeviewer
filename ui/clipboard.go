package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CopyToClipboard copies text to the system clipboard using the platform's
// native clipboard utility. Returns an error if no utility is available.
func CopyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		// Linux / FreeBSD — try common utilities in priority order.
		switch {
		case commandExists("wl-copy"):
			cmd = exec.Command("wl-copy")
		case commandExists("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
		case commandExists("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--input")
		default:
			return fmt.Errorf("no clipboard utility found (install wl-copy, xclip, or xsel)")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}
	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
