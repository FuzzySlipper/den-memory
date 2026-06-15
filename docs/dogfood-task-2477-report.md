# Den Memories v0 cross-runtime dogfood report

Result: `pass`

Query: `plugin`

## Recall packet handles
- service: packet `recall-1`, recall_log `1`, slugs `['curated-plugin-x-postmortem-candidate', 'plugin-x-project-a-failure-assessment']`
- hermes: packet `recall-2`, recall_log `2`, slugs `['curated-plugin-x-postmortem-candidate', 'plugin-x-project-a-failure-assessment']`
- pi_crew: packet `recall-3`, recall_log `3`, slugs `['curated-plugin-x-postmortem-candidate', 'plugin-x-project-a-failure-assessment']`

## Capture and curation handles
- seed.entries: `{'plugin': 1, 'den_core_policy': 2, 'hermes_invariant': 3, 'pi_worker': 4, 'reviewer_risk': 5, 'implementation_prereq': 6}`
- seed.supersede_event_id: `8`
- seed.noisy_capture_event_id: `1`
- seed.noisy_candidate_id: `1`
- seed.noisy_reject_event_id: `9`
- seed.promoted_capture_event_id: `2`
- seed.promoted_candidate_id: `2`
- seed.promote_event_id: `10`
- seed.promoted_entry_id: `8`
- seed.intentional_issue_entry_id: `9`
- hermes capture: `3`
- pi worker default capture decision: `ignored` reason `metadata_only`
- pi permissive capture event: `5`

## Independent audit
- audit export: `/tmp/den-memory-dogfood-2477/audit-export.jsonl`
- audit report: `/tmp/den-memory-dogfood-2477/independent-audit-report.md`
- doctor issue kinds: `['missing_source_refs', 'broken_or_unverified_source_refs', 'unscoped_candidates', 'secret_like_strings']`

## Dogfood findings
- {'severity': 'medium', 'kind': 'secret_marker_false_positive_or_extra_issue', 'expected_ref': 'entry:9', 'unexpected_refs': ['entry:4']}
