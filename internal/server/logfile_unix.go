//go:build !windows

package server

import "os"

func openLogFile(path string) (*os.File, error) {
	return os.Open(path)
}
