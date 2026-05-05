//go:build !unix

package stdio

import "syscall"

// Non-unix fallback: no pgid wrangling.

func setpgid() *syscall.SysProcAttr { return nil }

func killGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}
