//go:build !linux

package researchcompare

import "errors"

func lockFinalization(string) (func(), error) {
	return nil, errors.New("finalization locking requires Linux")
}
