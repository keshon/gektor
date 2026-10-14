//go:build !windows

package process

import "os"

type owner struct{}

func newOwner() (*owner, error)         { return &owner{}, nil }
func (*owner) attach(*os.Process) error { return nil }
func (*owner) close()                   {}
