package argocd

// password.go ports the BCrypt.hashpw(password, BCrypt.gensalt(4))
// invocation from ArgoCD.groovy.
//
// The Groovy code reaches for org.springframework.security.crypto.bcrypt.BCrypt
// (which itself is a thin wrapper over the canonical jBCrypt implementation
// by Damien Miller, OpenBSD-style $2a$). We use golang.org/x/crypto/bcrypt
// here; it produces the same $2a$<cost>$<salt+hash> format and accepts the
// same cost factor. ArgoCD validates the password with the same library on
// the server side, so the on-the-wire format must be identical.
//
// Cost factor: the Groovy original passes 4 (BCrypt.gensalt(4)). The default
// in golang.org/x/crypto/bcrypt is 10 – we explicitly set it to 4 so the
// generated hashes are byte-for-byte interchangeable with the existing
// Groovy install.
//
// See DEPS.md for the module reference; golang.org/x/crypto comes in as a
// transitive of go-git/v5, so no new direct dependency lands here.

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// argoCDAdminBcryptCost mirrors the Groovy BCrypt.gensalt(4).
const argoCDAdminBcryptCost = 4

// HashAdminPassword hashes the ArgoCD admin password for use in
// argocd-secret.stringData["admin.password"]. Empty input returns an
// error – the runner should refuse to install ArgoCD without one.
func HashAdminPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("argocd: admin password is empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), argoCDAdminBcryptCost)
	if err != nil {
		return "", fmt.Errorf("argocd: bcrypt: %w", err)
	}
	return string(hash), nil
}

// VerifyAdminPassword is the inverse of HashAdminPassword. It is here so
// tests can assert that the produced hash is well-formed without
// depending on the bcrypt package directly.
func VerifyAdminPassword(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
