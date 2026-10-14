//go:build !linux

package linux

func realIfaddrs(string) []string { return nil }
