package vless

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"hash"
)

// UUID versions.
const (
	_  byte = iota
	V1      // Version 1 (date-time and MAC address)
	_       // Version 2 (date-time and MAC address, DCE security version) [removed]
	V3      // Version 3 (namespace name-based)
	V4      // Version 4 (random)
	V5      // Version 5 (namespace name-based)
	V6      // Version 6 (k-sortable timestamp and random data, field-compatible with v1) [peabody draft]
	V7      // Version 7 (k-sortable timestamp and random data) [peabody draft]
	_       // Version 8 (k-sortable timestamp, meant for custom implementations) [peabody draft] [not implemented]
)

// UUID layout variants.
const (
	VariantNCS byte = iota
	VariantRFC4122
	VariantMicrosoft
	VariantFuture
)

type UUID [16]byte

var errInvalidFormat = errors.New("uuid: invalid UUID format")

func fromHexChar(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 255
}

// Parse parses the UUID stored in the string text. Parsing and supported
// formats are the same as UnmarshalText.
func (u *UUID) Parse(s string) error {
	switch len(s) {
	case 32: // hash
	case 36: // canonical
	case 34, 38:
		if s[0] != '{' || s[len(s)-1] != '}' {
			return fmt.Errorf("uuid: incorrect UUID format in string %q", s)
		}
		s = s[1 : len(s)-1]
	case 41, 45:
		if s[:9] != "urn:uuid:" {
			return fmt.Errorf("uuid: incorrect UUID format in string %q", s[:9])
		}
		s = s[9:]
	default:
		return fmt.Errorf("uuid: incorrect UUID length %d in string %q", len(s), s)
	}
	// canonical
	if len(s) == 36 {
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return fmt.Errorf("uuid: incorrect UUID format in string %q", s)
		}
		for i, x := range [16]byte{
			0, 2, 4, 6,
			9, 11,
			14, 16,
			19, 21,
			24, 26, 28, 30, 32, 34,
		} {
			v1 := fromHexChar(s[x])
			v2 := fromHexChar(s[x+1])
			if v1|v2 == 255 {
				return errInvalidFormat
			}
			u[i] = (v1 << 4) | v2
		}
		return nil
	}
	// hash like
	for i := 0; i < 32; i += 2 {
		v1 := fromHexChar(s[i])
		v2 := fromHexChar(s[i+1])
		if v1|v2 == 255 {
			return errInvalidFormat
		}
		u[i/2] = (v1 << 4) | v2
	}
	return nil
}

// Bytes returns a byte slice representation of the UUID.
func (u UUID) Bytes() []byte {
	return u[:]
}

// SetVersion sets the version bits.
func (u *UUID) SetVersion(v byte) {
	u[6] = (u[6] & 0x0f) | (v << 4)
}

// SetVariant sets the variant bits.
func (u *UUID) SetVariant(v byte) {
	switch v {
	case VariantNCS:
		u[8] = (u[8]&(0xff>>1) | (0x00 << 7))
	case VariantRFC4122:
		u[8] = (u[8]&(0xff>>2) | (0x02 << 6))
	case VariantMicrosoft:
		u[8] = (u[8]&(0xff>>3) | (0x06 << 5))
	case VariantFuture:
		fallthrough
	default:
		u[8] = (u[8]&(0xff>>3) | (0x07 << 5))
	}
}

// FromString returns a UUID parsed from the input string.
// Input is expected in a form accepted by UnmarshalText.
func FromString(text string) (UUID, error) {
	var u UUID
	err := u.Parse(text)
	return u, err
}

// Returns the UUID based on the hashing of the namespace UUID and name.
func newFromHash(h hash.Hash, ns UUID, name string) UUID {
	u := UUID{}
	h.Write(ns[:])
	h.Write([]byte(name))
	copy(u[:], h.Sum(nil))

	return u
}

func NewV5(ns UUID, name string) UUID {
	u := newFromHash(sha1.New(), ns, name)
	u.SetVersion(V5)
	u.SetVariant(VariantRFC4122)

	return u
}

var uuidNamespace, _ = FromString("00000000-0000-0000-0000-000000000000")

// UUIDMap https://github.com/XTLS/Xray-core/issues/158#issue-783294090
func UUIDMap(str string) (UUID, error) {
	u, err := FromString(str)
	if err != nil {
		return NewV5(uuidNamespace, str), nil
	}
	return u, nil
}
