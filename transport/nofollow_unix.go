//go:build !windows

package transport

import "golang.org/x/sys/unix"

// oNoFollow makes opening a symlink fail, closing the gap after the Lstat
// check.
const oNoFollow = unix.O_NOFOLLOW
