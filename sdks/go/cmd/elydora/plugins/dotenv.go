package plugins

import (
	"regexp"
	"strings"
)

var dotenvLinePattern = regexp.MustCompile(
	"(?m)^\\s*(export\\s+)?([\\w.-]+)(\\s*=\\s*?|:\\s+?)" +
		"(\\s*'(\\\\'|[^'])*'|\\s*\"(\\\\\"|[^\"])*\"|" +
		"\\s*`(\\\\`|[^`])*`|[^#\\r\\n]+)?\\s*(#.*)?$",
)

func parseDotenv(source []byte) map[string]string {
	values := map[string]string{}
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, match := range dotenvLinePattern.FindAllStringSubmatch(normalized, -1) {
		key := match[2]
		value := strings.TrimSpace(match[4])
		quote := byte(0)
		if value != "" {
			quote = value[0]
		}
		if len(value) >= 2 && (quote == '\'' || quote == '"' || quote == '`') && value[len(value)-1] == quote {
			value = value[1 : len(value)-1]
		}
		if quote == '"' {
			value = strings.ReplaceAll(value, `\n`, "\n")
			value = strings.ReplaceAll(value, `\r`, "\r")
		}
		values[key] = value
	}
	return values
}
