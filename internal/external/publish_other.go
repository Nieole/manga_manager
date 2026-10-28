//go:build !windows && !linux && !darwin

package external

import "os"

func publishMissing(source, destination string) error {
	if err := os.Link(source, destination); err != nil {
		return err
	}
	_ = os.Remove(source)
	return nil
}
