package convergence

// Severity ranks a finding's remediation urgency. Severity describes the work
// being corrected and is unrelated to task complexity, which continues to route
// task execution independently.
type Severity string

const (
	// SeverityLow is the least urgent severity.
	SeverityLow Severity = "low"
	// SeverityMedium is a moderate severity.
	SeverityMedium Severity = "medium"
	// SeverityHigh is an urgent severity.
	SeverityHigh Severity = "high"
	// SeverityCritical is the most urgent severity.
	SeverityCritical Severity = "critical"
)

// severityRank orders severities so the highest can be selected for a batch route.
var severityRank = map[Severity]int{
	SeverityLow:      1,
	SeverityMedium:   2,
	SeverityHigh:     3,
	SeverityCritical: 4,
}

// IsValid reports whether s is a recognized severity.
func (s Severity) IsValid() bool {
	_, ok := severityRank[s]
	return ok
}

// Rank returns the ordering weight of s, higher meaning more urgent. Unknown
// severities rank 0.
func (s Severity) Rank() int { return severityRank[s] }

// HigherThan reports whether s is strictly more urgent than other.
func (s Severity) HigherThan(other Severity) bool { return s.Rank() > other.Rank() }

// HighestSeverity returns the most urgent severity in a batch. It returns an
// empty severity and false when the batch is empty. Unknown severities are
// ignored so that malformed input cannot silently outrank a real severity.
func HighestSeverity(severities []Severity) (Severity, bool) {
	var best Severity
	found := false
	for _, candidate := range severities {
		if !candidate.IsValid() {
			continue
		}
		if !found || candidate.HigherThan(best) {
			best = candidate
			found = true
		}
	}
	return best, found
}
