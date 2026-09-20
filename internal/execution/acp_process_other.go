//go:build !linux

package execution

import (
	"context"
	"io"
)

func withACPProcess(context.Context, string, []string, []string, string, int,
	func(context.Context, io.Writer, io.Reader) error) error {
	return ErrUnsupported
}
