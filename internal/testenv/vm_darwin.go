package testenv

import "golang.org/x/sys/unix"

func virtualMac() bool {
	v, err := unix.SysctlUint32("kern.hv_vmm_present")
	return err == nil && v == 1
}
