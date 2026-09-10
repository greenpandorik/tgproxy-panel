//go:build !linux && !darwin

package agent

import "errors"

func exchangeDirectories(a, b string) error {
	return errors.New("atomic directory exchange is unsupported on this platform")
}
