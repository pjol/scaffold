// Package aauth implements the AAuth protocol primitives: identifiers, JOSE,
// typed tokens, RFC 9530 Content-Digest, RFC 9421 HTTP Message Signatures,
// Signature-Key, the seven-step request verifier, outbound signing, discovery
// with the JWKS cache and egress admission, requirement headers, problem
// responses, deferred responses, interaction codes, directed subject
// derivation and the wire types shared by the Person Server and the
// Supervision Server.
//
// It holds no database state and imports nothing above internal/safehttp. The
// Person Server (internal/ps) and Supervision Server (internal/ss) both build
// on it and never import each other, so anything they exchange is typed here.
//
// The normative text is pinned in docs/spec/. When this package's behaviour is
// not obvious, the comment quotes the section that demands it.
//
// Three rules recur and are worth knowing before using the package:
//
//   - Every protocol JSON body ignores members it does not recognize. Decode
//     with encoding/json directly; httpx.Decode rejects unknown fields and must
//     not be used on a protocol endpoint.
//   - A token in the Signature-Key header fails as 401 with a Signature-Error
//     code (TokenError.Sig). The same token carried as a body parameter fails
//     with that parameter's own code (TokenError.Code). The verifier functions
//     here set the right one for the context they serve.
//   - Identifiers are compared as exact strings. Nothing is case-folded or
//     normalized, except the @authority component, which RFC 9421 requires to
//     be normalized.
package aauth
