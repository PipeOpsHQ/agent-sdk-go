package secops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ParseTrivyReportInput struct {
	ReportJSON json.RawMessage
}

type Vulnerability struct {
	VulnerabilityID  string `json:"vulnerabilityId"`
	PkgName          string `json:"pkgName"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	FixedVersion     string `json:"fixedVersion,omitempty"`
	Severity         string `json:"severity"`
	Title            string `json:"title,omitempty"`
}

type CategorizedVulnerabilities struct {
	ArtifactName string          `json:"artifactName"`
	Critical     []Vulnerability `json:"critical"`
	High         []Vulnerability `json:"high"`
	MediumCount  int             `json:"mediumCount"`
	LowCount     int             `json:"lowCount"`
	TotalCount   int             `json:"totalCount"`
}

type RedactionResult struct {
	RedactedLogs string `json:"redactedLogs"`
	Redactions   int    `json:"redactions"`
}

type ClassifiedLogs struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	Info     []string `json:"info"`
}

type trivyReport struct {
	ArtifactName string        `json:"ArtifactName"`
	Results      []trivyResult `json:"Results"`
}

type trivyResult struct {
	Vulnerabilities []trivyVulnerability `json:"Vulnerabilities"`
}

type trivyVulnerability struct {
	VulnerabilityID  string `json:"VulnerabilityID"`
	PkgName          string `json:"PkgName"`
	InstalledVersion string `json:"InstalledVersion"`
	FixedVersion     string `json:"FixedVersion"`
	Severity         string `json:"Severity"`
	Title            string `json:"Title"`
}

var (
	sensitivePairPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|authorization)\b\s*([:=])\s*([^\s,;]+)`)
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9\-._~+/]+=*`)
)

func ParseTrivyReport(input ParseTrivyReportInput) (CategorizedVulnerabilities, error) {
	raw := bytes.TrimSpace(input.ReportJSON)
	if len(raw) == 0 {
		return CategorizedVulnerabilities{}, fmt.Errorf("trivy report payload is required")
	}

	// Allow callers to pass either JSON object bytes or a JSON-encoded string.
	if len(raw) > 0 && raw[0] == '"' {
		var embedded string
		if err := json.Unmarshal(raw, &embedded); err != nil {
			return CategorizedVulnerabilities{}, fmt.Errorf("decode trivy string payload: %w", err)
		}
		raw = []byte(strings.TrimSpace(embedded))
	}

	var report trivyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return CategorizedVulnerabilities{}, fmt.Errorf("decode trivy report: %w", err)
	}

	out := CategorizedVulnerabilities{
		ArtifactName: strings.TrimSpace(report.ArtifactName),
		Critical:     []Vulnerability{},
		High:         []Vulnerability{},
	}
	for _, result := range report.Results {
		for _, vuln := range result.Vulnerabilities {
			item := Vulnerability{
				VulnerabilityID:  strings.TrimSpace(vuln.VulnerabilityID),
				PkgName:          strings.TrimSpace(vuln.PkgName),
				InstalledVersion: strings.TrimSpace(vuln.InstalledVersion),
				FixedVersion:     strings.TrimSpace(vuln.FixedVersion),
				Severity:         strings.ToUpper(strings.TrimSpace(vuln.Severity)),
				Title:            strings.TrimSpace(vuln.Title),
			}
			out.TotalCount++
			switch item.Severity {
			case "CRITICAL":
				out.Critical = append(out.Critical, item)
			case "HIGH":
				out.High = append(out.High, item)
			case "MEDIUM":
				out.MediumCount++
			default:
				out.LowCount++
			}
		}
	}
	return out, nil
}

func RedactSensitiveData(logs string) RedactionResult {
	redacted := strings.TrimSpace(logs)
	if redacted == "" {
		return RedactionResult{RedactedLogs: "", Redactions: 0}
	}
	redactions := 0

	redacted = sensitivePairPattern.ReplaceAllStringFunc(redacted, func(match string) string {
		redactions++
		parts := sensitivePairPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return "[REDACTED]"
		}
		return fmt.Sprintf("%s%s [REDACTED]", parts[1], parts[2])
	})

	redacted = bearerPattern.ReplaceAllStringFunc(redacted, func(_ string) string {
		redactions++
		return "Bearer [REDACTED]"
	})

	return RedactionResult{
		RedactedLogs: redacted,
		Redactions:   redactions,
	}
}

func ClassifyLogEntries(logs string) ClassifiedLogs {
	out := ClassifiedLogs{
		Errors:   []string{},
		Warnings: []string{},
		Info:     []string{},
	}

	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		lower := strings.ToLower(entry)
		switch {
		case strings.Contains(lower, "panic"), strings.Contains(lower, "fatal"), strings.Contains(lower, "error"):
			out.Errors = append(out.Errors, entry)
		case strings.Contains(lower, "warn"):
			out.Warnings = append(out.Warnings, entry)
		default:
			out.Info = append(out.Info, entry)
		}
	}

	return out
}
