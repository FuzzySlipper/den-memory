package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"den-memories/internal/audit"
)

func main() {
	exportFile := flag.String("export-file", "", "Path to JSONL from /api/audit/export?format=jsonl")
	serviceURL := flag.String("service-url", "", "Den Memories service URL; reads /api/audit/export only")
	toolList := flag.String("tool-list", "", "Optional JSON file containing runtime tool names")
	profilePath := flag.String("profile", "", "Optional JSON config readback for the auditor profile")
	markdownOut := flag.String("markdown-out", "", "Optional path to write markdown report")
	timeout := flag.Float64("timeout", 5.0, "HTTP timeout in seconds")
	flag.Parse()

	profile := audit.DefaultProfile()
	if *profilePath != "" {
		if err := readJSONFile(*profilePath, &profile); err != nil {
			fail(2, "read profile: %v", err)
		}
	}
	tools, err := readTools(*toolList)
	if err != nil {
		fail(2, "read tool list: %v", err)
	}
	profileReport := audit.ValidateProfile(profile, tools)
	if profileReport["ok"] != true {
		data, _ := json.MarshalIndent(map[string]any{"profile_ok": false, "profile_report": profileReport}, "", "  ")
		fmt.Fprintln(os.Stderr, string(data))
		os.Exit(2)
	}
	exportText, err := readExport(*exportFile, *serviceURL, time.Duration(*timeout*float64(time.Second)))
	if err != nil {
		fail(2, "%v", err)
	}
	records, err := audit.RecordsFromJSONL(exportText)
	if err != nil {
		fail(2, "parse export: %v", err)
	}
	report := audit.ReportFromRecords(records)
	markdown := audit.Markdown(report)
	if *markdownOut != "" {
		if err := os.WriteFile(*markdownOut, []byte(markdown), 0o644); err != nil {
			fail(2, "write markdown: %v", err)
		}
	}
	fmt.Print(markdown)
	if report["ok"] == true {
		return
	}
	os.Exit(1)
}

func readExport(exportFile string, serviceURL string, timeout time.Duration) (io.Reader, error) {
	if exportFile != "" {
		file, err := os.Open(exportFile)
		if err != nil {
			return nil, err
		}
		return file, nil
	}
	if serviceURL == "" {
		return nil, fmt.Errorf("provide --export-file or --service-url")
	}
	client := http.Client{Timeout: timeout}
	resp, err := client.Get(serviceURL + "/api/audit/export?format=jsonl")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("audit export HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

func readTools(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	var loaded any
	if err := readJSONFile(path, &loaded); err != nil {
		return nil, err
	}
	if items, ok := loaded.([]any); ok {
		return stringsFromAny(items), nil
	}
	if obj, ok := loaded.(map[string]any); ok {
		if items, ok := obj["tools"].([]any); ok {
			return stringsFromAny(items), nil
		}
	}
	return nil, nil
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func stringsFromAny(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
