package plugins

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

func validateManagedPrivateKey(value string) error {
	seed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(seed) != 32 || base64.RawURLEncoding.EncodeToString(seed) != value {
		return fmt.Errorf("private key must be a canonical 32-byte base64url value")
	}
	return nil
}

func validateManagedBaseURL(value string) error {
	if strings.ContainsRune(value, '\\') {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	for _, character := range value {
		if character < 32 || unicode.IsSpace(character) {
			return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	validScheme := strings.EqualFold(parsed.Scheme, "http") ||
		strings.EqualFold(parsed.Scheme, "https")
	if !validScheme || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf(
			"base URL must exclude credentials, query parameters, and fragments",
		)
	}
	port := parsed.Port()
	if strings.HasSuffix(parsed.Host, ":") {
		return fmt.Errorf("base URL must contain a valid port")
	}
	if port != "" {
		value, convertErr := strconv.Atoi(port)
		if convertErr != nil || value < 1 || value > 65535 {
			return fmt.Errorf("base URL must contain a valid port")
		}
	}
	return nil
}
