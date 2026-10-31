package wallets

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Address is a 20-byte EVM address. The database stores the lowercase hex
// form (unique, comparable); the API sends the EIP-55 checksummed form.
type Address [20]byte

var errBadAddress = errors.New("not an EVM address")

// ParseAddress accepts "0x" + 40 hex digits. All-lowercase and all-uppercase
// inputs are taken as-is; mixed case must carry a valid EIP-55 checksum, which
// catches mistyped addresses.
func ParseAddress(s string) (Address, error) {
	var a Address
	s = strings.TrimSpace(s)
	if len(s) != 42 || (s[:2] != "0x" && s[:2] != "0X") {
		return a, errBadAddress
	}
	body := s[2:]
	b, err := hex.DecodeString(body)
	if err != nil {
		return a, errBadAddress
	}
	copy(a[:], b)
	if body != strings.ToLower(body) && body != strings.ToUpper(body) && a.Hex()[2:] != body {
		return a, errors.New("address checksum does not match")
	}
	return a, nil
}

// Lower is the lowercase 0x form stored in the database.
func (a Address) Lower() string { return "0x" + hex.EncodeToString(a[:]) }

// Hex is the EIP-55 checksummed form.
func (a Address) Hex() string {
	lower := hex.EncodeToString(a[:])
	hash := keccak256([]byte(lower))
	out := []byte(lower)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'f' {
			nibble := hash[i/2]
			if i%2 == 0 {
				nibble >>= 4
			}
			if nibble&0x0f >= 8 {
				out[i] -= 'a' - 'A'
			}
		}
	}
	return "0x" + string(out)
}

// Short is "0x1234…abcd" for notification copy.
func (a Address) Short() string {
	h := a.Hex()
	return h[:6] + "…" + h[len(h)-4:]
}

// checksum converts a stored lowercase address to EIP-55; unparseable input
// is returned unchanged.
func checksum(lower string) string {
	a, err := ParseAddress(lower)
	if err != nil {
		return lower
	}
	return a.Hex()
}

func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// personalHash is the EIP-191 version 0x45 hash that personal_sign signs:
// keccak256("\x19Ethereum Signed Message:\n" + len(msg) + msg).
func personalHash(msg []byte) []byte {
	return keccak256([]byte("\x19Ethereum Signed Message:\n"+strconv.Itoa(len(msg))), msg)
}

// recoverAddress returns the address whose key produced a 65-byte r||s||v
// signature over hash. v may be 0/1 or 27/28.
func recoverAddress(hash, sig []byte) (Address, error) {
	var a Address
	if len(sig) != 65 {
		return a, fmt.Errorf("signature is %d bytes, want 65", len(sig))
	}
	v := sig[64]
	if v >= 27 {
		v -= 27
	}
	if v > 1 {
		return a, errors.New("signature recovery id out of range")
	}
	// decred's compact form is <27 + recovery id> || r || s.
	compact := make([]byte, 65)
	compact[0] = 27 + v
	copy(compact[1:], sig[:64])
	pub, _, err := ecdsa.RecoverCompact(compact, hash)
	if err != nil {
		return a, err
	}
	uncompressed := pub.SerializeUncompressed() // 0x04 || X || Y
	copy(a[:], keccak256(uncompressed[1:])[12:])
	return a, nil
}

// decodeHex decodes an optional-0x hex string.
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

// hexQuantity parses a JSON-RPC quantity or 32-byte word ("0x1a") into a
// non-negative integer.
func hexQuantity(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	body, ok := strings.CutPrefix(s, "0x")
	if !ok || body == "" {
		return nil, fmt.Errorf("not a hex quantity: %q", s)
	}
	n, ok := new(big.Int).SetString(body, 16)
	if !ok {
		return nil, fmt.Errorf("not a hex quantity: %q", s)
	}
	return n, nil
}

// parseBaseUnits validates a non-negative integer amount in base units as a
// decimal string (fits numeric(78,0)).
func parseBaseUnits(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 78 {
		return nil, errors.New("amount must be 1 to 78 digits")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, errors.New("amount must be a whole number of base units")
		}
	}
	n, _ := new(big.Int).SetString(s, 10)
	return n, nil
}

// formatUnits renders base units as a decimal string with decimals places,
// trailing zeros trimmed ("1500000", 6 → "1.5"). For notification copy only;
// clients format with their own helpers.
func formatUnits(amount string, decimals int) string {
	amount = strings.TrimLeft(amount, "0")
	if amount == "" {
		return "0"
	}
	if decimals <= 0 {
		return amount
	}
	if len(amount) <= decimals {
		amount = strings.Repeat("0", decimals-len(amount)+1) + amount
	}
	whole, frac := amount[:len(amount)-decimals], strings.TrimRight(amount[len(amount)-decimals:], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}
