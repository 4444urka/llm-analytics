package security

import (
	"os/exec"
	"regexp"
	"strings"
)

var (
	dangerousPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(ignore|forget|disregard)\s+(all\s+)?(previous|above|prior|earlier)\s+(instructions?|prompts?|rules?|constraints?|guidelines?)`),
		regexp.MustCompile(`(?i)(you\s+are\s+now|act\s+as|pretend\s+you\s+are|roleplay\s+as)\s+(a\s+)?(different|new|other)`),
		regexp.MustCompile(`(?i)(system\s*:\s*|system\s+prompt\s*:\s*|system\s+message\s*:\s*)`),
		regexp.MustCompile(`(?i)(\[INST\]|<<SYS>>|<\|im_start\|>|<\|im_end\|>)`),
		regexp.MustCompile(`(?i)(do\s+not\s+(follow|obey|listen|adhere))`),
		regexp.MustCompile(`(?i)(override|overwrite|replace)\s+(system|instructions?|prompts?|rules?)`),
		regexp.MustCompile(`(?i)new\s+(system\s+)?(instructions?|prompts?|directives?)`),
		regexp.MustCompile(`(?i)you\s+must\s+(not\s+)?(analyze|respond|answer)`),
	}

	dangerousKeywords = []string{
		"DAN", "jailbreak", "prompt injection", "prompt leak",
		"system prompt", "---", "```system",
	}

	injectionPattern = regexp.MustCompile(`(?i)(\b(?:rm\s+-rf|sudo|chmod|wget\s+|curl\s+|/bin/bash|/bin/sh|exec\s*\(|eval\s*\(|os\.system|__import__|compile\()\b)`)
)

func SanitizeInput(input string) string {
	if len(input) > 10000 {
		input = input[:10000]
	}
	return input
}

func DetectPromptInjection(input string) bool {
	lower := strings.ToLower(input)

	for _, kw := range dangerousKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}

	for _, pat := range dangerousPatterns {
		if pat.MatchString(input) {
			return true
		}
	}

	return false
}

func SanitizeInstructions(input string) (string, bool) {
	input = SanitizeInput(input)
	if DetectPromptInjection(input) {
		return "", true
	}
	return input, false
}

func DetectCodeInjection(code string) bool {
	return injectionPattern.MatchString(code)
}

func CheckPython(pythonBin string) error {
	_, err := exec.LookPath(pythonBin)
	return err
}
