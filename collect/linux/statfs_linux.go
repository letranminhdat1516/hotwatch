//go:build linux

package linux

import "syscall"

// realStatfs reports total and free (including root-reserved) bytes from
// statfs(2); used = total - free, as df computes it.
func realStatfs(mount string) (total, free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(mount, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return st.Blocks * bs, st.Bfree * bs, nil
}
