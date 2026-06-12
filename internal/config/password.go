package config

import (
	"crypto/rand"
	"math/big"
)

// cryptoRand returns a uniformly distributed random int64 in [0, max).
// Backed by crypto/rand.
func cryptoRand(max int) (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}
