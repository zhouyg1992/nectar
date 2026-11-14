package memory

import "regexp"

var (
	secretAssignment = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|authorization)\b\s*[:=]\s*["']?[^\s"']+["']?`)
	bearerToken      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	privateKeyBlock  = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	knownKey         = regexp.MustCompile(`\b(sk-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})\b`)
)

// Redact removes common credential shapes before persistence. It is a safety
// net, not a substitute for avoiding secrets in chat in the first place.
func Redact(content string) string {
	content = privateKeyBlock.ReplaceAllString(content, "[REDACTED PRIVATE KEY]")
	content = bearerToken.ReplaceAllString(content, "Bearer [REDACTED]")
	content = secretAssignment.ReplaceAllString(content, "$1=[REDACTED]")
	content = knownKey.ReplaceAllString(content, "[REDACTED]")
	return content
}
