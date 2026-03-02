package elydora

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"io"
	"time"
)

// base64urlEncode encodes bytes to base64url without padding (RFC 4648 section 5).
func base64urlEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// base64urlDecode decodes a base64url string (with or without padding).
func base64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// generateNonce generates a 16-byte cryptographically random nonce, base64url encoded.
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64urlEncode(b), nil
}

// generateUUIDv7 generates a UUIDv7 (time-ordered) as a string.
func generateUUIDv7() (string, error) {
	now := time.Now()
	ms := now.UnixMilli()

	var uuid [16]byte

	// Timestamp: 48 bits in big-endian
	binary.BigEndian.PutUint16(uuid[0:2], uint16(ms>>32))
	binary.BigEndian.PutUint16(uuid[2:4], uint16(ms>>16))
	binary.BigEndian.PutUint16(uuid[4:6], uint16(ms))

	// Fill the rest with random data
	if _, err := io.ReadFull(rand.Reader, uuid[6:]); err != nil {
		return "", err
	}

	// Set version 7 (bits 48-51)
	uuid[6] = (uuid[6] & 0x0F) | 0x70
	// Set variant 10 (bits 64-65)
	uuid[8] = (uuid[8] & 0x3F) | 0x80

	return formatUUID(uuid), nil
}

// formatUUID formats a 16-byte UUID as a canonical string.
func formatUUID(uuid [16]byte) string {
	buf := make([]byte, 36)
	hex := "0123456789abcdef"
	positions := [16]int{0, 2, 4, 6, 9, 11, 14, 16, 19, 21, 24, 26, 28, 30, 32, 34}
	for i, p := range positions {
		buf[p] = hex[uuid[i]>>4]
		buf[p+1] = hex[uuid[i]&0x0F]
	}
	buf[8] = '-'
	buf[13] = '-'
	buf[18] = '-'
	buf[23] = '-'
	return string(buf)
}

// nowUnixMs returns the current time as Unix milliseconds.
func nowUnixMs() int64 {
	return time.Now().UnixMilli()
}
