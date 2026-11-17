# Interop fixtures

Static proof that this package and the published AAuth SDK agree on the wire, so the suite needs no Node. All keys are throwaway, generated for these files, and protect nothing.

| File | Made by | Holds |
|---|---|---|
| `sdk-signed.json` | `@aauth/agent` 4.1.1, `@aauth/protocol` 2.0.0, `@hellocoop/httpsig` 2.6.0, `jose` 6.2.15 on Node 22.21.0, 2026-11-05 | Requests signed by the SDK, header fields it built, thumbprints and digests it computed |
| `go-signed.json` | `TestInteropWriteGoFixtures`, go1.27.3, 2026-11-05 | Requests, responses, tokens and header fields from this package, with the keys that made them |
| `js-verdicts.json` | The same SDK packages, 2026-11-05 | What the SDK said about exactly the bytes of `go-signed.json`, tied to them by SHA-256 |

Each file also states its provenance in a `provenance` member.

## What the tests prove

- `TestInteropSDKRequestsVerify`: every SDK-signed request verifies under `Verifier`, or fails with the code its case names. Covers Ed25519 and ES256, `jwt` and `jwks_uri`, with and without a body, and real HTTP captures from a loopback server.
- `TestInteropSDKNegatives`: tampering with an SDK request fails, and moving the injected clock outside the window fails, so the clock is what the static fixtures rest on.
- `TestInteropSDKQueryDifference`: pins the one signature-base difference below to its exact cause.
- `TestInteropSDKHeadersAndPrimitives`: SDK-built headers parse here and serialize identically; thumbprints and digests match.
- `TestInteropGoSignerUnchanged`: the signer still produces what the SDK judged, byte for byte where the algorithm is deterministic. A wire change fails here, which is the prompt to regenerate.
- `TestInteropSDKVerdictsOnGoFixtures`: the SDK accepted all of it, and built the same signature base for each response.

## How the files are regenerated

Scripts are not kept in the repo, because they load third-party packages and have to stay outside it. Regenerating needs the person's go-ahead to install and run those packages. The steps:

1. Install the packages into a new empty directory outside the repo, with lifecycle scripts off: `npm install --ignore-scripts @aauth/agent @aauth/fetch`. Treat it as untrusted: never run anything with it as the working directory.
2. In a separate directory, write scripts that load the packages by absolute path with `import(pathToFileURL(...))`. Use Node 22. Stay offline except for your own loopback server. Don't import `@aauth/hardware-keys` or the keychain paths of `@aauth/local-keys`, and don't touch `~/.aauth`.
3. `AAUTH_INTEROP_WRITE=1 go test ./internal/aauth -run TestInteropWriteGoFixtures` rewrites `go-signed.json` with fresh keys.
4. Write `sdk-signed.json`, in the shape `sdkFile` in `interop_test.go` reads:
   - Generate keys with `jose.generateKeyPair` and `exportJWK`, then `publicJwkWithAlg` from `@aauth/local-keys/dist/jwk-alg.js`. That is how `agent-token.js` makes them; the function itself needs a keychain.
   - Mint agent tokens with `jose.SignJWT`, with the claims and header `signWithSoftwareKey` uses.
   - Replace `globalThis.fetch` with a capture and call the real `createSignedFetch(getKeyMaterial, { signBody: true })` and `@hellocoop/httpsig` `fetch`. Also run a few through a real loopback `http.createServer`, keeping `rawHeaders`.
5. Write `js-verdicts.json`, in the shape `jsVerdicts` reads:
   - Pin `Date.now` to the file's `now`, and serve the file's `issuers` from a stubbed `fetch`.
   - Requests: `@hellocoop/httpsig` `verify`. Tokens: `jose.jwtVerify`.
   - Responses: `verify` can't take them (below), so call it to record the refusal in `responses_high_level`, then verify with the library's own `parseDictionary`, `serializeItem`, `serializeInnerList`, `parseSignatureKey`, `generateContentDigest` and `crypto.verify`, walking the covered components in the script.
   - Store the SHA-256 of `go-signed.json` as `go_signed_sha256`.

If the SDK disagrees with the pinned spec, the spec wins. Record the difference here; don't bend the library.

## Known differences from the pinned spec in these SDK versions

| Where | SDK | Spec |
|---|---|---|
| `@query` component, signing and verifying | `"@query": x=1` | RFC 9421 section 2.2.7: `"@query": ?x=1`. Our verifier answers `invalid_signature` to an agent covering `@query`. The SDK doesn't cover it by default, so normal traffic is unaffected. |
| `verify()` on a signed response | Refuses: `Unsupported component parameters`; it also has no `@status` value | Supervision, Signed Decisions, requires `@status` and `;req`. A JS PS can't use `verify()` on our decisions. Its lower-level parts do verify them. |
| `Signature-Error` code for a key with no `alg`, or `alg: EdDSA` | `invalid_key` | Protocol, Verification, step 6: `unsupported_algorithm`, with `Accept-Signature-Alg` |
| `Signature-Error` code for `kty: oct` or `alg: HS256` | `invalid_key` | We answer `unsupported_algorithm`: step 6, a key type not implemented. The spec requires rejection and names no separate code. |
| `Signature-Key` with two members | Refused: `must have exactly one dictionary member` | Signature-key draft, Multiple Signatures, allows them. We select the member by label and ignore the rest. |
| `Signature-Key` parameters as Tokens: `sig=jwt;jwt=a.b.c` | Accepted | The draft says String. We answer `invalid_key`. |
| `buildRequirementHeader` and `parseRequirementHeader` for `requirement=interaction` with no `url`, or with an `http` URL carrying a query | Accepted | Protocol, Interaction Required: `url` is required, https, with no query or fragment. `Requirement.Field` and `Requirement.Validate` refuse them. |
| `verify({requireContentDigest:true})` on a body whose signature omits `content-type` | Accepted | Protocol, Covered Components: both are required at a PS or AS. We answer `invalid_input`. |

The live run against the running PS (`docs/CONFORMANCE.md`, Live interop) found four more, in `@aauth/agent` 4.1.1:

| Where | SDK | Spec |
|---|---|---|
| `pollDeferred`, first poll | Polls straight after a `202`, whatever `Retry-After` said | Deferred Responses: "The agent MUST respect Retry-After values." The PS holds a poll carrying `Prefer: wait` and doesn't answer it `slow_down`, so the SDK's first poll works here. |
| `pollDeferred` on `429` | Ends the request: `PersonTokenError 429` | Deferred Responses: "If the server responds with 429 Too Many Requests, the agent MUST increase its polling interval by 5 seconds". |
| `pollDeferred`, answering a clarification | POSTs `{"clarification_response": ...}` with no `action`, ignores the answer and polls again, asking `onClarification` each time | Agent Response to Clarification: "A POST body MUST include an action member. A server MUST reject a POST with a missing or unrecognized action with 400 Bad Request." The PS does. So the SDK can't complete a clarification against a conforming server. |
| `handleResourceInteraction` with a relative `Location` | `Invalid URL` | Deferred Responses: the `Location` is "on the same origin", and the spec's own example at a resource is `Location: /pending/f7a3b9c`. The SDK resolves one from a PS and not from a resource, so the testbed resource sends an absolute URL. |
