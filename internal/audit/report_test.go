package audit

import (
	"strings"
	"testing"
)

func TestValidateProfileRejectsRecallToolAndEnabledProvider(t *testing.T) {
	profile := DefaultProfile()
	profile["memory"] = map[string]any{
		"provider": "den_memory",
		"den_memory": map[string]any{
			"enabled":         true,
			"auto_recall":     true,
			"capture_on_sync": false,
			"prefetch":        false,
		},
	}

	report := ValidateProfile(profile, []string{"den_memory_recall", "GET /api/audit/export"})

	if report["ok"] == true {
		t.Fatalf("ValidateProfile unexpectedly passed: %#v", report)
	}
}

func TestReportFromRecordsFlagsContaminatedMetadata(t *testing.T) {
	records, err := RecordsFromJSONL(strings.NewReader(`{"record_type":"metadata","recall_used":true,"auditor_constraints":{"den_memory_provider_enabled":true,"recall_tools_allowed":true,"reads_via_audit_surfaces_only":false}}
{"record_type":"doctor","read_only":true,"issues":[]}
`))
	if err != nil {
		t.Fatalf("RecordsFromJSONL: %v", err)
	}

	report := ReportFromRecords(records)

	if report["ok"] == true {
		t.Fatalf("ReportFromRecords unexpectedly passed: %#v", report)
	}
	if !strings.Contains(Markdown(report), "export_recall_used_not_false") {
		t.Fatalf("Markdown missing contamination risk: %s", Markdown(report))
	}
}
