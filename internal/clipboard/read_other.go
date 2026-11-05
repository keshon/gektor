//go:build !windows

package clipboard

func ReadImage() ([]byte, error) { return nil, ErrUnsupported }
