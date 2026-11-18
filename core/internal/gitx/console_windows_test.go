//go:build windows

package gitx

import (
	"os/exec"
	"testing"
)

func TestHideConsoleSetsFlags(t *testing.T) {
	cmd := exec.Command("git", "status")
	hideConsole(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags != 0x08000000 {
		t.Fatalf("SysProcAttr = %+v, want HideWindow and CREATE_NO_WINDOW", cmd.SysProcAttr)
	}
}
