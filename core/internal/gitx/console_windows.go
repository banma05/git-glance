//go:build windows

package gitx

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

// hideConsole keeps a git child from opening a console window when the core
// itself runs without one.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
