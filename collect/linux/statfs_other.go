//go:build !linux

package linux

import "errors"

func realStatfs(string) (uint64, uint64, error) { return 0, 0, errors.New("statfs: not linux") }
