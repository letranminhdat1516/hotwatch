//go:build !(darwin && arm64)

package sensors

// Read reports no sensors on this platform.
func Read() ([]Reading, error) { return nil, nil }
