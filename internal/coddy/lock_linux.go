//go:build linux

package coddy

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func (s *Store) lock() (func(), error) {
	file, err := os.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open connector lock")
	}
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		file.Close()
		return nil, errors.New("state directory is in use by another connector")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
