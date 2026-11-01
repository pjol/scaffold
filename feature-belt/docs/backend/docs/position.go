package docs

import (
	"errors"
	"strings"
)

// Fractional indexing: positions are base-62 strings that sort in byte order
// (the columns use COLLATE "C"), and a new key always fits between any two,
// so inserting or moving a block rewrites only that block. This is the
// algorithm of the fractional-indexing package (rocicorp), which the shared
// TS module implements identically (docsKeyBetween).

const keyDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var errBadKey = errors.New("docs: invalid position key")

const maxKeyLen = 64

func intLen(head byte) (int, error) {
	switch {
	case head >= 'a' && head <= 'z':
		return int(head-'a') + 2, nil
	case head >= 'A' && head <= 'Z':
		return int('Z'-head) + 2, nil
	}
	return 0, errBadKey
}

func intPart(key string) (string, error) {
	if key == "" {
		return "", errBadKey
	}
	n, err := intLen(key[0])
	if err != nil || n > len(key) {
		return "", errBadKey
	}
	return key[:n], nil
}

// validKey reports whether key is a well-formed position.
func validKey(key string) bool {
	if key == "" || len(key) > maxKeyLen || key == "A"+strings.Repeat("0", 26) {
		return false
	}
	for i := 0; i < len(key); i++ {
		if strings.IndexByte(keyDigits, key[i]) < 0 {
			return false
		}
	}
	i, err := intPart(key)
	if err != nil {
		return false
	}
	f := key[len(i):]
	return f == "" || f[len(f)-1] != '0'
}

func midpoint(a, b string, hasB bool) string {
	if hasB {
		n := 0
		for {
			ca := byte('0')
			if n < len(a) {
				ca = a[n]
			}
			if n >= len(b) || ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			return b[:n] + midpoint(a[min(n, len(a)):], b[n:], true)
		}
	}
	digitA := 0
	if a != "" {
		digitA = strings.IndexByte(keyDigits, a[0])
	}
	digitB := len(keyDigits)
	if hasB && b != "" {
		digitB = strings.IndexByte(keyDigits, b[0])
	}
	if digitB-digitA > 1 {
		return string(keyDigits[(digitA+digitB+1)/2])
	}
	if hasB && len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if a != "" {
		rest = a[1:]
	}
	return string(keyDigits[digitA]) + midpoint(rest, "", false)
}

func incrementInt(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	carry := true
	for i := len(digs) - 1; carry && i >= 0; i-- {
		d := strings.IndexByte(keyDigits, digs[i]) + 1
		if d == len(keyDigits) {
			digs[i] = '0'
		} else {
			digs[i] = keyDigits[d]
			carry = false
		}
	}
	if !carry {
		return string(head) + string(digs), true
	}
	if head == 'Z' {
		return "a0", true
	}
	if head == 'z' {
		return "", false
	}
	h := head + 1
	if h > 'a' {
		digs = append(digs, '0')
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}

func decrementInt(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	borrow := true
	for i := len(digs) - 1; borrow && i >= 0; i-- {
		d := strings.IndexByte(keyDigits, digs[i]) - 1
		if d == -1 {
			digs[i] = 'z'
		} else {
			digs[i] = keyDigits[d]
			borrow = false
		}
	}
	if !borrow {
		return string(head) + string(digs), true
	}
	if head == 'a' {
		return "Zz", true
	}
	if head == 'A' {
		return "", false
	}
	h := head - 1
	if h < 'Z' {
		digs = append(digs, 'z')
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}

// keyBetween returns a position strictly between a and b ("" means open on
// that side). When a ≥ b (two siblings share a key), it falls back to a key
// after a.
func keyBetween(a, b string) string {
	if a != "" && !validKey(a) {
		a = ""
	}
	if b != "" && !validKey(b) {
		b = ""
	}
	if a != "" && b != "" && a >= b {
		b = ""
	}
	switch {
	case a == "" && b == "":
		return "a0"
	case a == "":
		ib, _ := intPart(b)
		fb := b[len(ib):]
		if ib == "A"+strings.Repeat("0", 26) {
			return ib + midpoint("", fb, true)
		}
		if ib < b {
			return ib
		}
		if res, ok := decrementInt(ib); ok {
			return res
		}
		return ib + midpoint("", fb, true)
	case b == "":
		ia, _ := intPart(a)
		fa := a[len(ia):]
		if i, ok := incrementInt(ia); ok {
			return i
		}
		return ia + midpoint(fa, "", false)
	}
	ia, _ := intPart(a)
	fa := a[len(ia):]
	ib, _ := intPart(b)
	fb := b[len(ib):]
	if ia == ib {
		return ia + midpoint(fa, fb, true)
	}
	if i, ok := incrementInt(ia); ok && i < b {
		return i
	}
	return ia + midpoint(fa, "", false)
}

// keysAfter returns n increasing positions after a (and before b when set).
func keysAfter(a, b string, n int) []string {
	out := make([]string, 0, n)
	prev := a
	for range n {
		k := keyBetween(prev, b)
		out = append(out, k)
		prev = k
	}
	return out
}
