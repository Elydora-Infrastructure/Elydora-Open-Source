package elydora

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// sha256Base64url computes SHA-256 of the input string and returns base64url-encoded result.
func sha256Base64url(data string) string {
	h := sha256.Sum256([]byte(data))
	return base64urlEncode(h[:])
}

// sha256BytesBase64url computes SHA-256 of raw bytes and returns base64url-encoded result.
func sha256BytesBase64url(data []byte) string {
	h := sha256.Sum256(data)
	return base64urlEncode(h[:])
}

// computePayloadHash computes the SHA-256 hash of the JCS-canonicalized payload.
func computePayloadHash(payload interface{}) (string, error) {
	canonical, err := jcsCanonicalise(payload)
	if err != nil {
		return "", fmt.Errorf("jcs canonicalise payload: %w", err)
	}
	return sha256BytesBase64url([]byte(canonical)), nil
}

// signEd25519 signs data with an Ed25519 private key seed (base64url-encoded 32 bytes).
// Returns the signature as base64url.
func signEd25519(privateKeyBase64url string, data []byte) (string, error) {
	seed, err := base64urlDecode(privateKeyBase64url)
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return "", fmt.Errorf("private key seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	key := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(key, data)
	return base64urlEncode(sig), nil
}

// ---------------------------------------------------------------------------
// JCS Canonicalization (RFC 8785)
// ---------------------------------------------------------------------------

// jcsCanonicalise produces a JCS-canonicalized JSON string from a Go value.
// Object keys are sorted lexicographically, no whitespace is used, and numbers
// follow ES2015 serialization rules.
func jcsCanonicalise(value interface{}) (string, error) {
	var b strings.Builder
	if err := jcsWrite(&b, value); err != nil {
		return "", err
	}
	return b.String(), nil
}

func jcsWrite(b *strings.Builder, value interface{}) error {
	if value == nil {
		b.WriteString("null")
		return nil
	}

	switch v := value.(type) {
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		// Parse as float64 for ES2015-compatible serialization
		f, err := v.Float64()
		if err != nil {
			return err
		}
		b.WriteString(jcsFormatNumber(f))
	case float64:
		b.WriteString(jcsFormatNumber(v))
	case float32:
		b.WriteString(jcsFormatNumber(float64(v)))
	case int:
		b.WriteString(strconv.FormatInt(int64(v), 10))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case string:
		jcsWriteString(b, v)
	case []interface{}:
		b.WriteByte('[')
		for i, elem := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := jcsWrite(b, elem); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		first := true
		for _, k := range keys {
			val := v[k]
			if val == nil {
				// JCS includes null values but skips undefined; in Go there is no undefined
				// so we include null.
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			jcsWriteString(b, k)
			b.WriteByte(':')
			if err := jcsWrite(b, val); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		// Fallback: marshal to JSON, then re-parse and canonicalize
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("jcs: cannot marshal %T: %w", v, err)
		}
		var parsed interface{}
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		if err := dec.Decode(&parsed); err != nil {
			return fmt.Errorf("jcs: cannot re-parse %T: %w", v, err)
		}
		return jcsWrite(b, parsed)
	}
	return nil
}

// jcsWriteString writes a JSON string with minimal escaping per the JSON spec.
func jcsWriteString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// jcsFormatNumber formats a float64 according to ES2015 Number serialization (JCS/RFC 8785).
func jcsFormatNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	if f == 0 {
		return "0"
	}
	// If the number is an integer that fits in int64, format without decimal point
	if f == math.Trunc(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e20 {
		return strconv.FormatInt(int64(f), 10)
	}
	// Use the shortest representation that round-trips
	return strconv.FormatFloat(f, 'G', -1, 64)
}

// signEOR signs an EOR struct. It computes the canonical representation (all fields except signature),
// hashes it, and produces an Ed25519 signature.
func signEOR(eor *EOR, privateKeyBase64url string) (string, error) {
	// Build canonical EOR without signature field
	canonical := map[string]interface{}{
		"op_version":      eor.OpVersion,
		"operation_id":    eor.OperationID,
		"org_id":          eor.OrgID,
		"agent_id":        eor.AgentID,
		"issued_at":       eor.IssuedAt,
		"ttl_ms":          eor.TTLMs,
		"nonce":           eor.Nonce,
		"operation_type":  eor.OperationType,
		"subject":         eor.Subject,
		"action":          eor.Action,
		"payload":         eor.Payload,
		"payload_hash":    eor.PayloadHash,
		"prev_chain_hash": eor.PrevChainHash,
		"agent_pubkey_kid": eor.AgentPubkeyKID,
	}
	canonicalStr, err := jcsCanonicalise(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalise eor: %w", err)
	}
	return signEd25519(privateKeyBase64url, []byte(canonicalStr))
}
