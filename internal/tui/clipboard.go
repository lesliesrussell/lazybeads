// lb-cqf
package tui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// clipboardFunc copies text to the operator's clipboard. Tests swap it out.
type clipboardFunc func(string) error

var errNoClipboardTool = errors.New("no clipboard helper found")

// copyToClipboard prefers a native helper and falls back to OSC 52, which is
// what carries a copy back across an ssh session.
func copyToClipboard(text string) error {
	nativeErr := nativeCopy(text)
	if nativeErr == nil {
		return nil
	}
	oscErr := osc52Copy(text)
	if oscErr == nil {
		return nil
	}
	if errors.Is(nativeErr, errNoClipboardTool) {
		return fmt.Errorf("%w (and OSC 52 failed: %v)", errNoClipboardTool, oscErr)
	}
	return nativeErr
}

func nativeCopy(text string) error {
	argv := clipboardArgv()
	if len(argv) == 0 {
		return errNoClipboardTool
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

// clipboardArgv returns the first clipboard helper on PATH for this platform.
func clipboardArgv() []string {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		candidates = [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}
	for _, argv := range candidates {
		if path, err := exec.LookPath(argv[0]); err == nil {
			return append([]string{path}, argv[1:]...)
		}
	}
	return nil
}

// osc52Copy writes the terminal's own clipboard escape straight to the tty. The
// payload is base64 so nothing in an issue id can escape the sequence, and the
// sequence itself renders nothing, so it does not disturb the Bubble Tea frame.
func osc52Copy(text string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	_, err = fmt.Fprintf(tty, "\x1b]52;c;%s\x07", payload)
	return err
}
