package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var requiredRegistryKeys = []string{
	"layers", "scope_kinds", "discovery_scopes", "claim_strengths",
	"capture_decisions", "candidate_statuses", "curation_actions", "edge_relations",
	"source_validation_states", "capture_policy_modes",
}

var exampleSchemas = map[string]string{
	"runtime-context.example.json":  "runtime-context.schema.json",
	"source-ref.example.json":       "source-ref.schema.json",
	"candidate.example.json":        "candidate.schema.json",
	"capture-request.example.json":  "capture-request.schema.json",
	"capture-response.example.json": "capture-response.schema.json",
	"recall-request.example.json":   "recall-request.schema.json",
	"recall-packet.example.json":    "recall-packet.schema.json",
	"audit-export.example.json":     "audit-export.schema.json",
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("contract validation passed")
}

func run(root string) error {
	contract := filepath.Join(root, "contracts", "v0")
	examples := filepath.Join(root, "examples", "v0")
	if err := validateRegistry(filepath.Join(contract, "registry.json")); err != nil {
		return err
	}
	if err := validateScoring(filepath.Join(contract, "scoring-defaults.json")); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	schemasDir := filepath.Join(contract, "schemas")
	entries, err := os.ReadDir(schemasDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(schemasDir, entry.Name())
		var schema map[string]any
		if err := readJSON(path, &schema); err != nil {
			return err
		}
		if id, ok := schema["$id"].(string); ok {
			if err := compiler.AddResource(id, schema); err != nil {
				return fmt.Errorf("add schema %s: %w", id, err)
			}
		}
		if err := compiler.AddResource(entry.Name(), schema); err != nil {
			return fmt.Errorf("add schema %s: %w", entry.Name(), err)
		}
	}
	names := make([]string, 0, len(exampleSchemas))
	for name := range exampleSchemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, exampleName := range names {
		schemaName := exampleSchemas[exampleName]
		schema, err := compiler.Compile(schemaName)
		if err != nil {
			return fmt.Errorf("compile %s: %w", schemaName, err)
		}
		var data any
		if err := readJSON(filepath.Join(examples, exampleName), &data); err != nil {
			return err
		}
		if err := schema.Validate(data); err != nil {
			return fmt.Errorf("%s: %w", exampleName, err)
		}
	}
	return validateNegativeCases(compiler, examples)
}

func validateRegistry(path string) error {
	var raw map[string]any
	if err := readJSON(path, &raw); err != nil {
		return err
	}
	registry := map[string][]string{}
	for key, value := range raw {
		items, ok := value.([]any)
		if !ok {
			continue
		}
		registry[key] = stringsFromAny(items)
	}
	for _, key := range requiredRegistryKeys {
		values := registry[key]
		if len(values) == 0 {
			return fmt.Errorf("registry %s is empty", key)
		}
		seen := map[string]struct{}{}
		for _, value := range values {
			if _, ok := seen[value]; ok {
				return fmt.Errorf("registry %s has duplicate %q", key, value)
			}
			seen[value] = struct{}{}
		}
	}
	for key, value := range map[string]string{
		"layers":               "curated_fact",
		"discovery_scopes":     "global_discoverable",
		"claim_strengths":      "policy",
		"capture_policy_modes": "permissive_candidates",
		"capture_decisions":    "errored",
		"curation_actions":     "rescope",
		"edge_relations":       "contextual_assessment",
	} {
		if !contains(registry[key], value) {
			return fmt.Errorf("registry %s missing %s", key, value)
		}
	}
	return nil
}

func validateScoring(path string) error {
	var scoring map[string]any
	if err := readJSON(path, &scoring); err != nil {
		return err
	}
	if scoring["profile"] != "v0-default" {
		return fmt.Errorf("scoring profile = %v, want v0-default", scoring["profile"])
	}
	if !greater(scoring, "root_match", "authoritative_scope_match", "discoverable_scope_match") {
		return fmt.Errorf("authoritative score must exceed discoverable score")
	}
	if number(scoring, "penalties", "cross_scope_discovered_evidence") >= 0 {
		return fmt.Errorf("cross_scope_discovered_evidence must be negative")
	}
	if !greater(scoring, "claim_strength_modifier", "policy", "observation") {
		return fmt.Errorf("policy claim strength must exceed observation")
	}
	if scoring["readback_required"] != true {
		return fmt.Errorf("readback_required must be true")
	}
	return nil
}

func validateNegativeCases(compiler *jsonschema.Compiler, examples string) error {
	schema, err := compiler.Compile("candidate.schema.json")
	if err != nil {
		return err
	}
	var candidate map[string]any
	if err := readJSON(filepath.Join(examples, "candidate.example.json"), &candidate); err != nil {
		return err
	}
	common := candidate["common"].(map[string]any)
	common["layer"] = "captured_observation"
	if err := schema.Validate(candidate); err == nil {
		return fmt.Errorf("candidate schema accepted unknown layer")
	}
	auditSchema, err := compiler.Compile("audit-export.schema.json")
	if err != nil {
		return err
	}
	var audit map[string]any
	if err := readJSON(filepath.Join(examples, "audit-export.example.json"), &audit); err != nil {
		return err
	}
	audit["auditor_constraints"] = map[string]any{
		"den_memory_provider_enabled":   true,
		"recall_tools_allowed":          true,
		"reads_via_audit_surfaces_only": false,
	}
	if err := auditSchema.Validate(audit); err == nil {
		return fmt.Errorf("audit schema accepted unsafe auditor constraints")
	}
	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringsFromAny(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func number(root map[string]any, section string, key string) float64 {
	values, _ := root[section].(map[string]any)
	value, _ := values[key].(float64)
	return value
}

func greater(root map[string]any, section string, left string, right string) bool {
	return number(root, section, left) > number(root, section, right)
}
