// Implements: REQ-005.
// Per: ADR-0029.
// Discipline: C-14.

// Package signature — signature.go owns the Signer interface that every
// implementation in this package and downstream Pro packages must satisfy.
// Keeping the contract in its own file makes the extension surface obvious
// and lets callers depend on the interface alone without importing any
// concrete implementation.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package signature

// Signer is the provider-neutral payload-signing contract.
//
// Implementations must be safe for concurrent use. The encoding produced
// by Sign (hex, base64, etc.) is implementation-defined; callers treat
// it as an opaque string and pass it back to Verify unchanged.
type Signer interface {
	// Sign returns a signature string for payload. Two calls on the same
	// payload with the same key must return the same string — this
	// determinism is required by storage-then-verify flows.
	Sign(payload []byte) string

	// Verify reports whether signature matches the expected signature of
	// payload under the signer's key. The comparison MUST be
	// constant-time (e.g., subtle.ConstantTimeCompare) so timing
	// observers cannot recover the expected signature.
	//
	// Returns false (not an error) for malformed signature input —
	// callers cannot distinguish "wrong signature" from "garbage
	// signature" for security reasons.
	Verify(payload []byte, signature string) bool
}
