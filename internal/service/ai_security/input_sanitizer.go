package ai_security

import (
	"regexp"
	"strings"
)

// AttackPatterns are regex patterns that indicate prompt injection attempts
var AttackPatterns = []string{
	`(?i)ignore\s+(previous|all|above)\s+(instructions?|prompts?)`,
	`(?i)reset\s+(system\s+)?prompt`,
	`(?i)roleplay\s+as\s+(admin|root|system)`,
	`(?i)DAN\s+mode`,
	`(?i)pretend\s+you\s+are`,
	`(?i)you\s+are\s+now\s+(a\s+)?(different|new)`,
	`<\|im_end\|>`,
	`<\|im_start\|>`,
	`<\|endoftext\|>`,
	`(?i)forget\s+everything`,
	`(?i)new\s+instructions?`,
	`(?i)override\s+(system|security|prompt)`,
}

var htmlPattern = regexp.MustCompile(`<[^>]*>`)

// SanitizeInput cleans user input to prevent prompt injection.
// Returns the cleaned input and whether any attack was detected.
func SanitizeInput(input string) (string, bool) {
	if !IsEnabled(KeyInputSanitizer) {
		return input, false
	}

	cleaned := strings.TrimSpace(input)

	// Check against attack patterns
	for _, pattern := range AttackPatterns {
		if matched, _ := regexp.MatchString(pattern, cleaned); matched {
			return cleaned, true
		}
	}

	// Strip HTML tags
	cleaned = htmlPattern.ReplaceAllString(cleaned, "")

	// Normalize whitespace
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")

	// Enforce max input length (defense against DoS)
	if len(cleaned) > 50000 {
		cleaned = cleaned[:50000]
	}

	return cleaned, false
}
