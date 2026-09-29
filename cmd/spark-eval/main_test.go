package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readRecords(t *testing.T, path string) []record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var results []record
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var item record
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatal(err)
		}
		results = append(results, item)
	}
	return results
}

func TestSuiteRepeatsFailuresAndResume(t *testing.T) {
	dir := t.TempDir()
	suitePath := filepath.Join(dir, "suite.yaml")
	manifestPath := filepath.Join(dir, "manifest.yaml")
	output := filepath.Join(dir, "results.jsonl")
	spark := filepath.Join(dir, "spark")
	invocations := filepath.Join(dir, "invocations")
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: pass\n    prompt: answer\n  - id: fail\n    skill: review\n", 0644)
	writeFixture(t, manifestPath, "version: 1\nmodels:\n  qwen35-4b:\n    gguf: model.gguf\n    quantization: Q4_K_M\n    llama_cpp_version: b123\n    chat_template: qwen\n    context_size: 4096\n    reasoning_mode: disabled\n    sampling:\n      temperature: 0.2\n    hardware_notes: test machine\n", 0644)
	script := "#!/bin/sh\nprintf x >> \"$INVOCATIONS\"\ncase \"$*\" in\n*answer*) echo '{\"schema_version\":1,\"selected_profile\":\"qwen35-4b\",\"resolved_model\":\"resolved\",\"answer\":\"ok\",\"duration_ms\":12,\"usage\":{\"total_tokens\":5},\"rounds\":1,\"tool_calls\":[],\"status\":\"succeeded\",\"error\":\"\"}' ;;\n*) echo '{\"schema_version\":1,\"selected_profile\":\"qwen35-4b\",\"resolved_model\":\"resolved\",\"answer\":\"partial\",\"duration_ms\":7,\"usage\":{},\"rounds\":2,\"tool_calls\":[],\"status\":\"failed\",\"error\":\"endpoint unavailable\"}'; exit 1 ;;\nesac\n"
	writeFixture(t, spark, script, 0755)
	t.Setenv("INVOCATIONS", invocations)
	args := []string{"--suite", suitePath, "--manifest", manifestPath, "--model", "qwen35-4b", "--runs", "2", "--output", output, "--spark", spark}
	for pass := 0; pass < 2; pass++ {
		if err := run(context.Background(), args, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "2 evaluation run(s) failed") {
			t.Fatalf("want recorded failure summary, got %v", err)
		}
	}
	results := readRecords(t, output)
	if len(results) != 4 {
		t.Fatalf("got %d records", len(results))
	}
	ids := map[string]bool{}
	for _, item := range results {
		if ids[item.RunID] || item.SchemaVersion != 1 || item.EvaluatorVersion != "1" || item.SuiteID != "tiny" || item.ModelProfile != "qwen35-4b" || item.ResolvedModel != "resolved" || item.Metadata.GGUF != "model.gguf" || item.Metadata.ContextSize != 4096 || item.ManifestSHA256 == "" {
			t.Fatalf("bad record: %+v", item)
		}
		ids[item.RunID] = true
		if item.CaseID == "fail" && (item.Outcome != "failed" || item.Error != "endpoint unavailable" || item.Rounds != 2) {
			t.Fatalf("bad failure record: %+v", item)
		}
	}
	count, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatal(err)
	}
	if len(count) != 4 {
		t.Fatalf("Spark invoked %d times, want 4", len(count))
	}
}

func TestInterruptedCaseIsRecorded(t *testing.T) {
	dir := t.TempDir()
	suitePath, output, spark := filepath.Join(dir, "suite.yaml"), filepath.Join(dir, "results.jsonl"), filepath.Join(dir, "spark")
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: wait\n    prompt: wait\n", 0644)
	writeFixture(t, spark, "#!/bin/sh\nexec sleep 10\n", 0755)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := run(ctx, []string{"--suite", suitePath, "--model", "test", "--output", output, "--spark", spark}, &strings.Builder{})
	if err == nil {
		t.Fatal("expected interruption")
	}
	results := readRecords(t, output)
	if len(results) != 1 || results[0].Outcome != "interrupted" {
		t.Fatalf("interrupted record: %+v", results)
	}
}

func TestEndpointFailureWithoutJSONIsRecorded(t *testing.T) {
	dir := t.TempDir()
	suitePath, output, spark := filepath.Join(dir, "suite.yaml"), filepath.Join(dir, "results.jsonl"), filepath.Join(dir, "spark")
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: offline\n    prompt: check endpoint\n", 0644)
	writeFixture(t, spark, "#!/bin/sh\necho 'connection refused' >&2\nexit 1\n", 0755)
	err := run(context.Background(), []string{"--suite", suitePath, "--model", "test", "--output", output, "--spark", spark}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "1 evaluation run(s) failed") {
		t.Fatalf("expected failure summary, got %v", err)
	}
	results := readRecords(t, output)
	if len(results) != 1 || results[0].Outcome != "failed" || results[0].ExitCode != 1 || !strings.Contains(results[0].Stderr, "connection refused") {
		t.Fatalf("failure record: %+v", results)
	}
}

func TestInvalidSuiteAndCorruptResumeFile(t *testing.T) {
	dir := t.TempDir()
	suitePath, output := filepath.Join(dir, "suite.yaml"), filepath.Join(dir, "results.jsonl")
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: repeated\n    prompt: one\n  - id: repeated\n    prompt: two\n", 0644)
	args := []string{"--suite", suitePath, "--model", "test", "--output", output}
	if err := run(context.Background(), args, &strings.Builder{}); err == nil {
		t.Fatal("accepted duplicate case ids")
	}
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: one\n    prompt: hello\n", 0644)
	writeFixture(t, output, "{bad json}\n", 0644)
	if err := run(context.Background(), args, &strings.Builder{}); err == nil {
		t.Fatal("accepted corrupt result file")
	}
}

func TestPracticalSuiteIsRunnable(t *testing.T) {
	path := filepath.Join("..", "..", "evaluation", "suites", "practical.yaml")
	var s suite
	if _, err := readYAML(path, &s); err != nil {
		t.Fatal(err)
	}
	if err := validateSuite(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Cases) != 7 {
		t.Fatalf("practical suite has %d cases, want 7", len(s.Cases))
	}
	for _, c := range s.Cases {
		if c.Workspace == "" {
			t.Fatalf("case %q has no workspace", c.ID)
		}
		workspace := filepath.Join(filepath.Dir(path), c.Workspace)
		info, err := os.Stat(workspace)
		if err != nil || !info.IsDir() {
			t.Fatalf("case %q workspace %q is unavailable: %v", c.ID, workspace, err)
		}
	}
	dir := t.TempDir()
	spark := filepath.Join(dir, "spark")
	output := filepath.Join(dir, "runs.jsonl")
	writeFixture(t, spark, "#!/bin/sh\necho '{\"schema_version\":1,\"selected_profile\":\"test\",\"resolved_model\":\"fake\",\"answer\":\"fixture\",\"duration_ms\":1,\"usage\":{},\"rounds\":1,\"tool_calls\":[],\"status\":\"succeeded\",\"error\":\"\"}'\n", 0755)
	if err := run(context.Background(), []string{"--suite", path, "--model", "test", "--spark", spark, "--output", output}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	results := readRecords(t, output)
	if len(results) != len(s.Cases) {
		t.Fatalf("got %d practical results, want %d", len(results), len(s.Cases))
	}
	for index, item := range results {
		if item.CaseID != s.Cases[index].ID || item.Outcome != "succeeded" {
			t.Fatalf("case %d result: %+v", index, item)
		}
	}
}

func TestDefaultModelDiscoverySeparatesResumeKeys(t *testing.T) {
	dir := t.TempDir()
	modelID := "first.gguf"
	previousDiscover := discoverActiveModel
	discoverActiveModel = func(context.Context) (string, string, error) {
		return "default", modelID, nil
	}
	defer func() { discoverActiveModel = previousDiscover }()
	suitePath, output, spark := filepath.Join(dir, "suite.yaml"), filepath.Join(dir, "results.jsonl"), filepath.Join(dir, "spark")
	writeFixture(t, suitePath, "version: 1\nid: tiny\ncases:\n  - id: one\n    prompt: hello\n", 0644)
	writeFixture(t, spark, "#!/bin/sh\ncase \"$*\" in *--model*) exit 2 ;; esac\necho \"{\\\"schema_version\\\":1,\\\"selected_profile\\\":\\\"default\\\",\\\"resolved_model\\\":\\\"$MODEL_ID\\\",\\\"answer\\\":\\\"ok\\\",\\\"status\\\":\\\"succeeded\\\"}\"\n", 0755)
	args := []string{"--suite", suitePath, "--spark", spark, "--output", output}
	for _, id := range []string{"first.gguf", "second.gguf", "second.gguf"} {
		modelID = id
		t.Setenv("MODEL_ID", id)
		if err := run(context.Background(), args, &strings.Builder{}); err != nil {
			t.Fatal(err)
		}
	}
	records := readRecords(t, output)
	if len(records) != 2 || records[0].ResolvedModel != "first.gguf" || records[1].ResolvedModel != "second.gguf" || records[0].RunID == records[1].RunID {
		t.Fatalf("default model records: %+v", records)
	}
	modelID = "third.gguf"
	t.Setenv("MODEL_ID", "different.gguf")
	if err := run(context.Background(), args, &strings.Builder{}); err == nil {
		t.Fatal("accepted a model change during evaluation")
	}
	records = readRecords(t, output)
	if len(records) != 3 || records[2].Outcome != "failed" || !strings.Contains(records[2].Error, "expected API model") {
		t.Fatalf("model change record: %+v", records)
	}
}
