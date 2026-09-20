//go:build !linux

package researchweb

import (
	"errors"
	"os"
)

func lockStore(string) (func(), error) { return nil, errors.New("research server requires Linux") }
func openSource(*os.Root, string) (*os.File, error) {
	return nil, errors.New("research server requires Linux")
}
