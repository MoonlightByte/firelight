//go:build !linux

package nativesave

import (
	"fmt"
	"os"
)

func platformSupported() error {
	return fmt.Errorf("native checkpoint storage is supported only by the verified Linux host")
}
func openSessionLock(string, bool) (*os.File, error) { return nil, platformSupported() }
func lockSession(*os.File) error                     { return platformSupported() }
func unlockSession(*os.File) error                   { return platformSupported() }
func openRegular(string) (*os.File, error)           { return nil, platformSupported() }
