package audit

import "crypto/rand"

// readRandom reads len(b) random bytes from crypto/rand.
func readRandom(b []byte) (int, error) {
	return rand.Read(b)
}
