package commands

import "github.com/DeprecatedLuar/dredge/internal/crypto"

func HandleLock() error {
	return crypto.ClearSession()
}
