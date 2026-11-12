//go:build !darwin && !linux

package service

import (
	"fmt"
	"runtime"
)

func New(string, string) (Service, error) {
	return nil, fmt.Errorf("background service management is not supported on %s", runtime.GOOS)
}
