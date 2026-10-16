//go:build !windows

package agent

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

func defaultConfigPath() string { return "/etc/hotwatch/agent.json" }

func checkPrivate(path string, st fs.FileInfo) error {
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is accessible by group or others (mode %04o); it must be 0600", path, st.Mode().Perm())
	}
	return nil
}

func restrictFile(f *os.File) error { return f.Chmod(0o600) }

// keepOwner gives f the owner and group of the file at path, if it exists.
func keepOwner(f *os.File, path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return f.Chown(int(sys.Uid), int(sys.Gid))
	}
	return nil
}
