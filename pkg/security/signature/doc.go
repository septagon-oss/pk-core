// Package signature defines PlatformKit's provider-neutral payload-signing
// contract and ships an HMAC-SHA256 default implementation.
//
// # The Signer interface
//
//	type Signer interface {
//	    Sign(payload []byte) string
//	    Verify(payload []byte, signature string) bool
//	}
//
// Verify is constant-time; callers MUST NOT compare signatures via `==` or
// bytes.Equal because that leaks timing information that lets an attacker
// recover the expected signature byte by byte.
//
// # Default implementation
//
//   - HMACSigner (NewHMACSigner(key)) — HMAC-SHA256, hex-encoded output.
//     Minimum key length is 32 bytes (256 bits); callers should generate
//     keys with crypto/rand and rotate them via deployment secrets.
//
// # Pro/downstream
//
// KMS-backed signers (where the signing key lives in a cloud HSM and never
// touches the application process), envelope-encrypted multi-region keys,
// and Tink-backed key rotation live in downstream Pro packages. They
// satisfy the same Signer interface so module wiring is unchanged.
//
// # What this package does NOT cover
//
// Public-key signatures (ed25519, RSA, ECDSA), JWS, COSE, and JWT signing
// are deliberately out of scope. They are different cryptographic
// primitives with different threat models — signing a JWT is the job of
// security/authn, not this package.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package signature
