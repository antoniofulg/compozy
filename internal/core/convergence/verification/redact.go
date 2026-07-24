package verification

import "regexp"

// redactionPlaceholder replaces a redacted secret value.
const redactionPlaceholder = "***"

// secretAssignment matches a secret-bearing key followed by its value in
// key=value or key: value form. It redacts the value while preserving the key,
// so a bounded summary or argv never leaks a credential.
var secretAssignment = regexp.MustCompile(
	`(?i)\b(password|passwd|secret|token|api[_-]?key|apikey|authorization|access[_-]?key|` +
		`credential|private[_-]?key)([=:]\s*)(\S+)`,
)

// secretFlagName matches a bare secret-bearing flag whose value is the next argv
// token (for example, "--token" followed by "abc123").
var secretFlagName = regexp.MustCompile(
	`(?i)^--?(password|passwd|secret|token|api[_-]?key|apikey|authorization|access[_-]?key|` +
		`credential|private[_-]?key)$`,
)

// redactSummary redacts secret-bearing assignments in a bounded output summary.
// Non-secret output is preserved verbatim so the summary stays useful.
func redactSummary(text string) string {
	return secretAssignment.ReplaceAllString(text, "$1$2"+redactionPlaceholder)
}

// redactArgv returns a copy of argv with secret-bearing values redacted, both in
// inline --flag=value form and in a value that follows a bare secret flag.
func redactArgv(argv []string) []string {
	redacted := make([]string, len(argv))
	redactNext := false
	for i, arg := range argv {
		switch {
		case redactNext:
			redacted[i] = redactionPlaceholder
			redactNext = false
		case secretFlagName.MatchString(arg):
			redacted[i] = arg
			redactNext = true
		default:
			redacted[i] = secretAssignment.ReplaceAllString(arg, "$1$2"+redactionPlaceholder)
		}
	}
	return redacted
}
