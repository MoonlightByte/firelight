//go:build linux

package nativesave

import (
	"fmt"
	"os"
	"syscall"
)

func platformSupported() error { return nil }
func openSessionLock(path string, create bool) (*os.File, error) {
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if create {
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("invalid campaign lock file: %v", err)
	}
	return f, nil
}
func lockSession(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func unlockSession(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func openRegular(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("archive member must be a regular file: %v", err)
	}
	return f, nil
}
