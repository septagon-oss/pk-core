// Package passhash defines PlatformKit's password hashing contract.
//
// # The Hasher interface
//
//	type Hasher interface {
//	    Hash(secret string) (string, error)
//	    Verify(secret, encoded string) error
//	}
//
// # Default implementations
//
//   - BcryptHasher (NewBcrypt(cost)) — long passwords; OWASP 2024+ cost floor 12
//   - Argon2idHasher (NewArgon2id(params)) — alternative for long passwords
//   - ShortSecretHasher (NewShortSecret(pepper)) — 2FA backup codes, recovery codes
//
// # Choosing
//
//   - Login passwords → BcryptHasher (or Argon2id if you have memory headroom)
//   - 2FA backup codes / single-use recovery codes → ShortSecretHasher
//
// # Pro/downstream
//
// HSM-backed and KMS-backed hashers (where the work happens in a hardware
// security module rather than the application process) live in downstream
// Pro packages. They satisfy the same Hasher interface so module wiring is
// unchanged.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package passhash
