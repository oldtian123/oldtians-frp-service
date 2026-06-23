package database

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random 128-bit identifier as a 32-char hex string. Used as the
// primary key for tunnels and event logs. It panics only if the system CSPRNG
// fails, which is treated as unrecoverable.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("database: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
