//go:build !windows

package runner

import "errors"

// Antigravity accounts of their own need the Windows credential agy reads;
// elsewhere agy runs only with the machine's own login.

const agyKeyringSupported = false

var errNoAgyKeyring = errors.New("contas extras do Antigravity só funcionam no Windows")

func readAgySlot() ([]byte, error) { return nil, errNoAgyKeyring }
func writeAgySlot([]byte) error    { return errNoAgyKeyring }
func deleteAgySlot() error         { return errNoAgyKeyring }
