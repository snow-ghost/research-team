package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestStartupBDD_ConfigurationDoesNotEnterRestartLoop(t *testing.T) {
	if exitCode(fmt.Errorf("load: %w", configurationError{errors.New("invalid configuration")})) != 78 {
		t.Fatal("permanent configuration failure was retryable")
	}
	if exitCode(errors.New("temporary database failure")) != 1 {
		t.Fatal("temporary startup failure was suppressed")
	}
}
