package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"den-memories/internal/curator"
)

func main() {
	var cfg curator.Config
	var candidateIDs string
	var mode string
	flag.StringVar(&cfg.BaseURL, "base-url", envDefault("DEN_MEMORY_URL", "http://127.0.0.1:8780"), "Den Memories service base URL")
	flag.StringVar(&mode, "mode", "deterministic", "curator mode; currently only deterministic")
	flag.StringVar(&cfg.Action, "action", "promote", "deterministic proposal action: promote, reject, or defer")
	flag.StringVar(&candidateIDs, "candidate-ids", "", "comma-separated candidate IDs to consider; empty means all queue items needing proposal")
	flag.IntVar(&cfg.Limit, "limit", 50, "maximum queue items to read")
	flag.StringVar(&cfg.ProposerIdentity, "proposer-identity", "den-memory-curator", "identity recorded on stored proposals")
	flag.StringVar(&cfg.ProposerKind, "proposer-kind", "deterministic_cli", "proposer kind recorded on stored proposals")
	flag.StringVar(&cfg.Reason, "reason", "deterministic curator proposal", "reason recorded on stored proposals")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "print proposals without storing them")
	flag.Parse()

	if mode != "deterministic" {
		fatalf("unsupported mode %q; only deterministic is available", mode)
	}
	ids, err := parseIDs(candidateIDs)
	if err != nil {
		fatalf("parse candidate ids: %v", err)
	}
	cfg.CandidateIDs = ids
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := curator.Run(ctx, cfg, nil)
	if err != nil {
		fatalf("curator run failed: %v", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatalf("encode result: %v", err)
	}
	fmt.Println(string(data))
}

func parseIDs(text string) ([]int, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	ids := []int{}
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, err := strconv.Atoi(item)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func envDefault(key string, fallback string) string {
	if value := os.Getenv(key); strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
