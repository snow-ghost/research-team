//go:build !linux

package execution

import (
	"context"
	"os"
)

func runProcess(context.Context, string, []string, []string, string, string, int) ([]byte, error) {
	return nil, ErrUnsupported
}

func openRegular(*os.Root, string) (*os.File, error) {
	return nil, ErrUnsupported
}
