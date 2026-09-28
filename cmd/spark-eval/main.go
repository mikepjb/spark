// spark-eval runs declarative cases through the real Spark JSON CLI.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const schemaVersion = 1
const evaluatorVersion = "1"

type suite struct {
	Version int        `yaml:"version"`
	ID      string     `yaml:"id"`
	Cases   []evalCase `yaml:"cases"`
}

type evalCase struct {
	ID        string `yaml:"id"`
	Prompt    string `yaml:"prompt"`
	Skill     string `yaml:"skill"`
	Workspace string `yaml:"workspace"`
}

type manifest struct {
	Version int                      `yaml:"version"`
	Models  map[string]modelMetadata `yaml:"models"`
}

type modelMetadata struct {
	GGUF            string         `yaml:"gguf" json:"gguf,omitempty"`
	Quantization    string         `yaml:"quantization" json:"quantization,omitempty"`
	LlamaCPPVersion string         `yaml:"llama_cpp_version" json:"llama_cpp_version,omitempty"`
	ChatTemplate    string         `yaml:"chat_template" json:"chat_template,omitempty"`
	ContextSize     int            `yaml:"context_size" json:"context_size,omitempty"`
	ReasoningMode   string         `yaml:"reasoning_mode" json:"reasoning_mode,omitempty"`
	Sampling        map[string]any `yaml:"sampling" json:"sampling,omitempty"`
	HardwareNotes   string         `yaml:"hardware_notes" json:"hardware_notes,omitempty"`
}

type cliResult struct {
	SchemaVersion   int             `json:"schema_version"`
	SelectedProfile string          `json:"selected_profile"`
	ResolvedModel   string          `json:"resolved_model"`
	Answer          string          `json:"answer"`
	DurationMS      int64           `json:"duration_ms"`
	Usage           json.RawMessage `json:"usage"`
	Rounds          int             `json:"rounds"`
	ToolCalls       json.RawMessage `json:"tool_calls"`
	Status          string          `json:"status"`
	Error           string          `json:"error"`
}

type record struct {
	SchemaVersion    int             `json:"schema_version"`
	EvaluatorVersion string          `json:"evaluator_version"`
	RunID            string          `json:"run_id"`
	SuiteID          string          `json:"suite_id"`
	SuiteSHA256      string          `json:"suite_sha256"`
	CaseID           string          `json:"case_id"`
	Prompt           string          `json:"prompt"`
	Skill            string          `json:"skill,omitempty"`
	Workspace        string          `json:"workspace"`
	Repetition       int             `json:"repetition"`
	ModelProfile     string          `json:"model_profile"`
	ResolvedModel    string          `json:"resolved_model"`
	Metadata         modelMetadata   `json:"metadata"`
	ManifestSHA256   string          `json:"manifest_sha256,omitempty"`
	Answer           string          `json:"answer"`
	Outcome          string          `json:"outcome"`
	DurationMS       int64           `json:"duration_ms"`
	Usage            json.RawMessage `json:"usage"`
	Rounds           int             `json:"rounds"`
	ToolCalls        json.RawMessage `json:"tool_calls"`
	ExitCode         int             `json:"exit_code"`
	Error            string          `json:"error,omitempty"`
	Stderr           string          `json:"stderr,omitempty"`
}

type options struct {
	suitePath, manifestPath, model, outputPath, sparkPath string
	runs                                                  int
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "spark-eval:", err)
		if ctx.Err() != nil {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func parse(args []string, stderr io.Writer) (options, error) {
	var o options
	f := flag.NewFlagSet("spark-eval", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.suitePath, "suite", "", "suite YAML path")
	f.StringVar(&o.manifestPath, "manifest", "", "optional inference metadata YAML path")
	f.StringVar(&o.model, "model", "", "configured Spark model profile")
	f.StringVar(&o.outputPath, "output", "evaluation/results/runs.jsonl", "append-only result file")
	f.StringVar(&o.sparkPath, "spark", "spark", "Spark executable path")
	f.IntVar(&o.runs, "runs", 1, "repetitions per case")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 || o.suitePath == "" || o.model == "" || o.runs < 1 || o.outputPath == "" || o.sparkPath == "" {
		return o, fmt.Errorf("requires --suite, --model, --runs >= 1, and no positional arguments")
	}
	return o, nil
}

func readYAML(path string, target any) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one YAML document")
	}
	return data, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateSuite(s suite) error {
	if s.Version != 1 || s.ID == "" || len(s.Cases) == 0 {
		return fmt.Errorf("suite requires version 1, id, and cases")
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if c.ID == "" || seen[c.ID] || (c.Prompt == "" && c.Skill == "") {
			return fmt.Errorf("case ids must be unique and each case needs a prompt or skill")
		}
		seen[c.ID] = true
	}
	return nil
}

func loadExisting(path string) (map[string]string, error) {
	completed := map[string]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return completed, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("result file has an incomplete final line: %s", path)
	}
	for lineNumber, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry struct {
			RunID   string `json:"run_id"`
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(line, &entry); err != nil || entry.RunID == "" || entry.Outcome == "" {
			return nil, fmt.Errorf("invalid result line %d", lineNumber+1)
		}
		if _, exists := completed[entry.RunID]; exists {
			return nil, fmt.Errorf("duplicate run_id on line %d", lineNumber+1)
		}
		completed[entry.RunID] = entry.Outcome
	}
	return completed, nil
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	o, err := parse(args, stderr)
	if err != nil {
		return err
	}
	var s suite
	suiteData, err := readYAML(o.suitePath, &s)
	if err != nil {
		return fmt.Errorf("read suite: %w", err)
	}
	if err := validateSuite(s); err != nil {
		return err
	}
	var metadata modelMetadata
	var manifestHash string
	if o.manifestPath != "" {
		var m manifest
		manifestData, err := readYAML(o.manifestPath, &m)
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		if m.Version != 1 {
			return fmt.Errorf("manifest requires version 1")
		}
		var ok bool
		metadata, ok = m.Models[o.model]
		if !ok {
			return fmt.Errorf("model %q is absent from manifest", o.model)
		}
		manifestHash = digest(manifestData)
	}
	spark := o.sparkPath
	if strings.ContainsRune(spark, os.PathSeparator) {
		spark, err = filepath.Abs(spark)
		if err != nil {
			return err
		}
	}
	completed, err := loadExisting(o.outputPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.outputPath), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(o.outputPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	suiteHash := digest(suiteData)
	failures := 0
	for _, c := range s.Cases {
		workspace := c.Workspace
		if workspace == "" {
			workspace, err = os.Getwd()
		} else if !filepath.IsAbs(workspace) {
			workspace = filepath.Join(filepath.Dir(o.suitePath), workspace)
		}
		if err != nil {
			return err
		}
		workspace, err = filepath.Abs(workspace)
		if err != nil {
			return err
		}
		info, err := os.Stat(workspace)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("case %q workspace is not a directory: %s", c.ID, workspace)
		}
		for repetition := 1; repetition <= o.runs; repetition++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			id := digest([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", s.ID, suiteHash, c.ID, workspace, o.model, manifestHash, repetition)))
			if outcome, exists := completed[id]; exists {
				if outcome != "succeeded" {
					failures++
				}
				continue
			}
			entry := execute(ctx, spark, s.ID, suiteHash, c, workspace, o.model, metadata, manifestHash, repetition, id)
			encoded, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			if _, err := file.Write(append(encoded, '\n')); err != nil {
				return err
			}
			if err := file.Sync(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(stderr, "%s/%s %s run %d: %s\n", s.ID, c.ID, o.model, repetition, entry.Outcome)
			if entry.Outcome != "succeeded" {
				failures++
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d evaluation run(s) failed; see %s", failures, o.outputPath)
	}
	return nil
}

func execute(ctx context.Context, spark, suiteID, suiteHash string, c evalCase, workspace, profile string, metadata modelMetadata, manifestHash string, repetition int, id string) record {
	entry := record{SchemaVersion: schemaVersion, EvaluatorVersion: evaluatorVersion, RunID: id, SuiteID: suiteID, SuiteSHA256: suiteHash, CaseID: c.ID, Prompt: c.Prompt, Skill: c.Skill, Workspace: workspace, Repetition: repetition, ModelProfile: profile, Metadata: metadata, ManifestSHA256: manifestHash, Outcome: "failed", Usage: json.RawMessage(`{}`), ToolCalls: json.RawMessage(`[]`)}
	args := []string{"--format", "json", "--model", profile}
	if c.Skill != "" {
		args = append(args, "--skill", c.Skill)
	}
	if c.Prompt != "" {
		args = append(args, "--prompt", c.Prompt)
	}
	command := exec.CommandContext(ctx, spark, args...)
	command.Dir = workspace
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	started := time.Now()
	err := command.Run()
	entry.DurationMS = time.Since(started).Milliseconds()
	entry.Stderr = strings.TrimSpace(stderr.String())
	if err != nil {
		entry.Error = err.Error()
		entry.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			entry.ExitCode = exitErr.ExitCode()
		}
	}
	if ctx.Err() != nil {
		entry.Outcome = "interrupted"
	}
	var result cliResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr == nil && result.SchemaVersion == 1 && validOutcome(result.Status) {
		entry.ResolvedModel = result.ResolvedModel
		entry.Answer = result.Answer
		entry.Usage = result.Usage
		entry.ToolCalls = result.ToolCalls
		if len(entry.Usage) == 0 {
			entry.Usage = json.RawMessage(`{}`)
		}
		if len(entry.ToolCalls) == 0 {
			entry.ToolCalls = json.RawMessage(`[]`)
		}
		entry.Rounds = result.Rounds
		entry.DurationMS = result.DurationMS
		entry.Outcome = result.Status
		entry.Error = result.Error
		if ctx.Err() != nil {
			entry.Outcome = "interrupted"
		}
		if result.SelectedProfile != profile {
			entry.Outcome = "failed"
			entry.Error = fmt.Sprintf("CLI selected profile %q, expected %q", result.SelectedProfile, profile)
		}
		if err != nil && entry.Outcome == "succeeded" {
			entry.Outcome = "failed"
			entry.Error = fmt.Sprintf("Spark exited %d despite success result", entry.ExitCode)
		}
	} else {
		entry.Error = fmt.Sprintf("invalid Spark JSON result (schema 1 required): %s", truncate(stdout.String(), 500))
		if len(stdout.Bytes()) == 0 && err != nil {
			entry.Error = err.Error()
		}
	}
	return entry
}

func validOutcome(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}
func truncate(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
