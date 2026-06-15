// Package audit implements memory-free audit export reporting.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Constraints are the required independent auditor memory-free constraints.
var Constraints = map[string]any{
	"den_memory_provider_enabled":   false,
	"recall_tools_allowed":          false,
	"reads_via_audit_surfaces_only": true,
}

var forbiddenTools = map[string]struct{}{
	"den_memory_recall":          {},
	"den_memory_search":          {},
	"den_memory_read":            {},
	"den_memory_store_candidate": {},
	"den_memory_capture_event":   {},
	"den_memory_link":            {},
	"den_memory_curate":          {},
}

var requiredRecordTypes = map[string]struct{}{
	"metadata":          {},
	"capture_events":    {},
	"memory_candidates": {},
	"curation_events":   {},
	"recall_logs":       {},
	"doctor":            {},
}

// DefaultProfile returns the memory-free independent auditor profile.
func DefaultProfile() map[string]any {
	return map[string]any{
		"profile_id": "den-memory-independent-auditor",
		"runtime_context": map[string]any{
			"runtime":      "auditor",
			"session_kind": "diagnostic",
			"role":         "auditor",
			"mode":         "audit",
			"project_id":   "den-memory",
		},
		"memory": map[string]any{
			"provider": "off",
			"den_memory": map[string]any{
				"enabled":         false,
				"auto_recall":     false,
				"capture_on_sync": false,
				"prefetch":        false,
			},
		},
	}
}

// ValidateProfile checks that an auditor profile cannot use Den Memories as background memory.
func ValidateProfile(profile map[string]any, tools []string) map[string]any {
	memory := object(profile["memory"])
	denMemory := object(memory["den_memory"])
	runtime := object(profile["runtime_context"])
	toolSet := map[string]struct{}{}
	for _, tool := range tools {
		toolSet[tool] = struct{}{}
	}
	presentForbidden := []string{}
	for tool := range forbiddenTools {
		if _, ok := toolSet[tool]; ok {
			presentForbidden = append(presentForbidden, tool)
		}
	}
	sort.Strings(presentForbidden)
	checks := []map[string]any{
		check("runtime_role_is_auditor", runtime["role"] == "auditor" && runtime["mode"] == "audit", fmt.Sprintf("role=%v mode=%v", runtime["role"], runtime["mode"])),
		check("den_memory_provider_disabled", providerDisabled(memory, denMemory), fmt.Sprintf("memory.provider=%v den_memory.enabled=%v", memory["provider"], denMemory["enabled"])),
		check("automatic_den_memory_paths_disabled", denMemory["auto_recall"] == false && denMemory["capture_on_sync"] == false && denMemory["prefetch"] == false, "auto_recall/capture_on_sync/prefetch must all be false"),
		check("forbidden_den_memory_tools_absent", len(presentForbidden) == 0, fmt.Sprintf("present_forbidden_tools=%v", presentForbidden)),
	}
	ok := true
	for _, item := range checks {
		if item["ok"] != true {
			ok = false
		}
	}
	return map[string]any{"ok": ok, "checks": checks, "auditor_constraints": Constraints}
}

// RecordsFromJSONL loads newline-delimited JSON records.
func RecordsFromJSONL(r io.Reader) ([]map[string]any, error) {
	scanner := bufio.NewScanner(r)
	records := []map[string]any{}
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(text), &record); err != nil {
			return nil, fmt.Errorf("invalid_jsonl_line:%d:%w", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// ReportFromRecords produces a compact independent audit report.
func ReportFromRecords(records []map[string]any) map[string]any {
	recordTypes := map[string]int{}
	for _, record := range records {
		recordTypes[fmt.Sprint(record["record_type"])]++
	}
	metadata := firstRecord(records, "metadata")
	doctorRecords := recordsOfType(records, "doctor")
	recallLogs := recordsOfType(records, "recall_logs")
	candidates := recordsOfType(records, "memory_candidates")

	risks := []map[string]any{}
	constraints := object(metadata["auditor_constraints"])
	if metadata["recall_used"] != false {
		risks = append(risks, risk("export_recall_used_not_false", "critical", metadata["recall_used"]))
	}
	mismatches := map[string]any{}
	for key, expected := range Constraints {
		if constraints[key] != expected {
			mismatches[key] = map[string]any{"expected": expected, "actual": constraints[key]}
		}
	}
	if len(mismatches) > 0 {
		risks = append(risks, risk("auditor_constraints_mismatch", "critical", mismatches))
	}
	missing := []string{}
	for required := range requiredRecordTypes {
		if recordTypes[required] == 0 {
			missing = append(missing, required)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		risks = append(risks, risk("missing_required_record_types", "warning", missing))
	}

	doctorIssues := []any{}
	for _, doctor := range doctorRecords {
		if items, ok := doctor["issues"].([]any); ok {
			doctorIssues = append(doctorIssues, items...)
		}
	}
	highSeverity := []any{}
	for _, item := range doctorIssues {
		issue := object(item)
		switch strings.ToLower(fmt.Sprint(issue["severity"])) {
		case "critical", "high", "error":
			highSeverity = append(highSeverity, item)
		}
	}
	unscoped := []any{}
	for _, candidate := range candidates {
		if candidate["scope_id"] == nil || candidate["authority_scope_id"] == nil {
			unscoped = append(unscoped, candidate["id"])
		}
	}
	ok := true
	for _, risk := range risks {
		if risk["severity"] == "critical" {
			ok = false
		}
	}
	return map[string]any{
		"ok":                  ok,
		"record_type_counts":  recordTypes,
		"auditor_constraints": Constraints,
		"contamination_risks": risks,
		"evidence_handles": map[string]any{
			"capture_event_ids":  ids(recordsOfType(records, "capture_events"), "id"),
			"candidate_ids":      ids(candidates, "id"),
			"curation_event_ids": ids(recordsOfType(records, "curation_events"), "id"),
			"recall_log_ids":     ids(recallLogs, "id"),
			"recall_packet_ids":  ids(recallLogs, "packet_id"),
		},
		"findings": map[string]any{
			"doctor_issue_count":          len(doctorIssues),
			"high_severity_doctor_issues": highSeverity,
			"unscoped_candidate_ids":      unscoped,
		},
	}
}

// Markdown renders an audit report in Markdown.
func Markdown(report map[string]any) string {
	lines := []string{"# Den Memories independent auditor report", ""}
	result := "fail"
	if report["ok"] == true {
		result = "pass"
	}
	lines = append(lines, fmt.Sprintf("Result: `%s`", result), "", "## Contamination-risk checks")
	risks, _ := report["contamination_risks"].([]map[string]any)
	if len(risks) == 0 {
		if generic, ok := report["contamination_risks"].([]any); ok {
			for _, item := range generic {
				risks = append(risks, object(item))
			}
		}
	}
	if len(risks) == 0 {
		lines = append(lines, "- none")
	} else {
		for _, risk := range risks {
			lines = append(lines, fmt.Sprintf("- **%s** (%s): `%v`", risk["kind"], risk["severity"], risk["detail"]))
		}
	}
	lines = append(lines, "", "## Evidence handles")
	for key, value := range object(report["evidence_handles"]) {
		lines = append(lines, fmt.Sprintf("- %s: %v", key, value))
	}
	lines = append(lines, "", "## Findings")
	findings := object(report["findings"])
	lines = append(lines, fmt.Sprintf("- doctor_issue_count: %v", findings["doctor_issue_count"]))
	lines = append(lines, fmt.Sprintf("- high_severity_doctor_issues: %v", findings["high_severity_doctor_issues"]))
	lines = append(lines, fmt.Sprintf("- unscoped_candidate_ids: %v", findings["unscoped_candidate_ids"]))
	return strings.Join(lines, "\n") + "\n"
}

func providerDisabled(memory map[string]any, denMemory map[string]any) bool {
	provider := memory["provider"]
	return (provider == nil || provider == "off" || provider == "none" || provider == "disabled") && denMemory["enabled"] == false
}

func check(name string, ok bool, detail string) map[string]any {
	return map[string]any{"name": name, "ok": ok, "detail": detail}
}

func risk(kind string, severity string, detail any) map[string]any {
	return map[string]any{"kind": kind, "severity": severity, "detail": detail}
}

func object(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

func firstRecord(records []map[string]any, recordType string) map[string]any {
	for _, record := range records {
		if record["record_type"] == recordType {
			return record
		}
	}
	return map[string]any{}
}

func recordsOfType(records []map[string]any, recordType string) []map[string]any {
	result := []map[string]any{}
	for _, record := range records {
		if record["record_type"] == recordType {
			result = append(result, record)
		}
	}
	return result
}

func ids(records []map[string]any, key string) []any {
	result := []any{}
	for _, record := range records {
		if value := record[key]; value != nil {
			result = append(result, value)
		}
	}
	return result
}
