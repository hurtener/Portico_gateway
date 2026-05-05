//go:build unix

package stdio

import "syscall"

func setpgid() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup signals -pid (the whole process group spawned by Setpgid).
// Falls back to signalling the single pid if pgid signalling fails.
func killGroup(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(-pid, sig); err == nil {
		return nil
	}
	return syscall.Kill(pid, sig)
}
