//go:build darwin

package agent

import "golang.org/x/sys/unix"

func exchangeDirectories(a, b string) error {
	return unix.RenamexNp(a, b, unix.RENAME_SWAP)
}
