//go:build windows

package secrets

import (
	"errors"
	"strings"
	"syscall"

	"github.com/danieljoos/wincred"
)

const targetPrefix = "Dragon/"

// WinCredStore stores credentials inside Windows Credential Manager.
type WinCredStore struct{}

// NewDefaultStore returns the Windows Credential Manager secret store.
func NewDefaultStore() Store {
	return &WinCredStore{}
}

func targetName(key string) string {
	return targetPrefix + key
}

func (w *WinCredStore) Get(key string) (string, error) {
	cred, err := wincred.GetGenericCredential(targetName(key))
	if err != nil {
		if isWinCredNotFound(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	val := string(cred.CredentialBlob)
	if val == "" {
		return "", ErrNotFound
	}
	return val, nil
}

func (w *WinCredStore) Set(key, value string) error {
	if value == "" {
		return w.Delete(key)
	}
	cred := wincred.NewGenericCredential(targetName(key))
	cred.UserName = "Dragon"
	cred.CredentialBlob = []byte(value)
	cred.Persist = wincred.PersistLocalMachine
	return cred.Write()
}

func (w *WinCredStore) Delete(key string) error {
	cred, err := wincred.GetGenericCredential(targetName(key))
	if err != nil {
		if isWinCredNotFound(err) {
			return nil
		}
		return err
	}
	return cred.Delete()
}

func (w *WinCredStore) Has(key string) bool {
	val, err := w.Get(key)
	return err == nil && val != ""
}

func isWinCredNotFound(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 1168 { // ERROR_NOT_FOUND
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "element not found") || strings.Contains(msg, "not found")
}
