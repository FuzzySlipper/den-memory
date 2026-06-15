# Den Memories independent auditor report

Result: `pass`

## Contamination-risk checks
- none

## Evidence handles
- capture_event_ids: [5, 4, 3, 2, 1]
- candidate_ids: [3, 2, 1]
- curation_event_ids: [11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1]
- recall_log_ids: [3, 2, 1]
- recall_packet_ids: ['recall-3', 'recall-2', 'recall-1']

## Findings
- doctor_issue_count: 4
- high_severity_doctor_issues: [{'count': 2, 'details': {'markers_checked': ['BEGIN PRIVATE KEY', 'BEGIN RSA PRIVATE KEY', 'api_key=', 'xoxb-', 'ghp_', 'sk-']}, 'ids': ['entry:4', 'entry:9'], 'kind': 'secret_like_strings', 'severity': 'critical', 'summary': 'Secret/token-like strings found in memory text'}]
- unscoped_candidate_ids: [1]
