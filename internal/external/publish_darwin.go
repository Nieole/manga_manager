package external

import "golang.org/x/sys/unix"

func publishMissing(source, destination string) error {
	return unix.RenamexNp(source, destination, unix.RENAME_EXCL)
}
