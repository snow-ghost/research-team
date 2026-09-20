//go:build !linux

package coddy

import "errors"

func (s *Store) lock() (func(), error) {
	return nil, errors.New("connector state locking is implemented for Linux")
}
