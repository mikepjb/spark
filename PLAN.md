# Spark development plan

This plan develops a measurable, non-interactive Spark workflow before adding
retrieval. Complete the milestones in order unless a milestone explicitly says
otherwise. Each delegation prompt is intended to work in a fresh context and
therefore asks the implementer to inspect the repository before changing it.

## Principles

- Keep Spark read-only and preserve the existing TUI behaviour.
- Use the same coordinator, prompts, skills, tools, and model configuration in
  interactive and non-interactive modes.
- Do not make Spark responsible for downloading, starting, or stopping models.
- Prefer objective fixtures and deterministic checks over an LLM judge.
- Record exact model and inference settings alongside every evaluation result.
- Treat retrieval as an experiment: do not add vector infrastructure until the
  evaluation shows that simpler document search is insufficient.

## Milestone 1: Single-shot CLI

Add a headless invocation path while retaining the TUI as the no-argument
default.

Target interface:

```text
spark
spark -p "explain the configuration flow"
spark -s review
spark -s review -p "focus on concurrency problems"
spark --model qwen-4b -p "review this package"
spark --format json -s review
```

Required behaviour:

- `-p` / `--prompt` submits one prompt and exits.
- `-s` / `--skill` activates one discovered skill for the request.
- A skill without a prompt gets a documented generic workspace prompt.
- `-m` / `--model` selects a configured model profile.
- `--format text|json` selects human or machine-readable output.
- Text mode streams answer content to stdout and sends diagnostics/tool progress
  to stderr.
- JSON mode emits one complete, versioned result object.
- SIGINT cancels the coordinator and exits with status 130.
- Usage/configuration failures have stable non-zero exit statuses.

Implementation guidance:

- Extract shared bootstrap work from `cmd/spark/main.go` instead of duplicating
  the application setup.
- Put argument parsing and headless rendering behind testable functions that
  accept explicit readers/writers.
- Consume the existing `repl.Coordinator` event stream.
- Preserve token usage in structured response/run data rather than relying only
  on transient context events.
- Include the selected profile, resolved model, answer, duration, usage, tool
  calls, status, and error in JSON output. Version the schema from its first
  release.

Acceptance criteria:

- Existing TUI tests and behaviour remain intact.
- All documented invocations have CLI tests.
- Skills and model selection behave the same in TUI and headless modes.
- stdout can be safely piped; diagnostics do not contaminate it.
- Cancellation, partial streams, model failures, and invalid flags are tested.

Delegation prompt:

> Inspect Spark's current bootstrap, command engine, coordinator events, and
> tests. Implement Milestone 1 from `PLAN.md`: the single-shot CLI, shared
> bootstrap, text/JSON output, usage/tool telemetry, cancellation, and tests.
> Preserve the no-argument TUI and Spark's read-only boundary. Run the relevant
> Go tests and report any deliberately deferred details.

## Milestone 2: Revision-range Git diff

Make code review useful for both uncommitted work and committed feature
branches. Extend the bounded Git tooling to support an explicitly validated
revision range such as `main...HEAD`, without permitting arbitrary shell
execution or option injection. Since repositories use different base branch
names, use the base named by the user instead of assuming `main`, `master`, or
`develop`. If a branch review has no clear base, its skill should ask.

Acceptance criteria:

- Existing staged/unstaged diff calls remain compatible.
- A caller can request a revision range and an optional workspace-relative path.
- The review skill uses a base named by the user, or asks when the intended base
  is unclear.
- Revisions beginning with `-`, invalid argument combinations, oversized output,
  timeouts, and non-repositories are handled safely.
- Tool definitions, implementation, tests, and user-facing documentation agree.
- `spark -s review` can review a branch when its skill requests a range.

Delegation prompt:

> Inspect Spark's Git tool definitions, validation, tests, and review-related
> documentation. Implement Milestone 2 from `PLAN.md`: safe revision-range
> support for GitDiff, including `main...HEAD`, while preserving existing
> staged/unstaged behaviour and workspace boundaries. Use the user-specified
> base in review requests and ask if unclear. Add adversarial tests, update
> documentation, run the relevant Go tests, and do not add shell access.

## Milestone 3: Evaluation runner and result format

Build a development-only evaluation runner on top of Spark's versioned JSON CLI
output. It should exercise the real entrypoint rather than reimplementing the
agent loop.

Proposed interface:

```text
go run ./cmd/spark-eval \
  --suite evaluation/suites/core.yaml \
  --model qwen35-4b \
  --runs 3
```

Each recorded run must include:

- Case and suite identifiers.
- Model profile and resolved model.
- GGUF/quantization and inference metadata supplied by the run manifest.
- llama.cpp version, chat template, context size, reasoning mode, sampling
  settings, and hardware notes when provided.
- Answer, outcome, duration, token usage, rounds, and tool trace.
- Evaluator version and schema version.

The runner must not manage model processes. It should fail clearly when a
configured endpoint is unavailable. Generated detailed results should be
ignored by default; deliberately curated summaries may be committed.

Acceptance criteria:

- A tiny fake suite can run in tests without a live model.
- Interrupted or failed cases are recorded rather than silently discarded.
- Repeated runs remain distinct and resumable.
- Output is stable enough to compare models and reasoning modes.

Delegation prompt:

> Inspect the completed headless CLI and repository conventions. Implement
> Milestone 3 from `PLAN.md`: a development evaluation runner that consumes the
> real versioned Spark JSON interface, reads declarative suites, supports repeat
> runs/model filters, records reproducibility metadata, and handles failures and
> resume safely. Do not manage model servers. Add fixture-based tests and usage
> documentation.

## Milestone 4: Core evaluation suite

Create small, reviewable fixture repositories and cases covering:

1. Direct-answer discipline, including prompts that should make zero tool calls.
2. Repository comprehension with known paths, symbols, and data flows.
3. Code review with seeded defects and false-positive tracking.
4. Tool discipline: necessary, unnecessary, malformed, repeated, and
   budget-exhausting call patterns.
5. External Go-library knowledge, including stable, obscure, recent, and
   version-specific APIs.
6. A small number of long-context cases with deliberately confusable evidence.

Prefer exact checks and structured expectations. Use a short blind human rubric
only where answer quality cannot be reduced to deterministic checks. Do not use
an LLM judge in the first suite.

Acceptance criteria:

- Every case explains what capability it measures and why its answer is known.
- Seeded review defects have stable identifiers and severities.
- Tool expectations distinguish required, allowed, and forbidden calls.
- Fixtures are small enough to audit and contain no network dependency.
- The suite reports task pass rate, review recall/false positives, unsupported
  claims where measurable, irrelevant calls, malformed calls, budget exhaustion,
  latency, tokens, and model rounds.

Delegation prompt:

> Read `PLAN.md`, the evaluation runner, Spark's tools, and existing tests.
> Implement Milestone 4: auditable fixture repositories and a core suite for
> direct-answer discipline, repository comprehension, seeded code review, tool
> discipline, external Go knowledge, and limited long-context behaviour. Prefer
> deterministic scoring, document every expected answer, avoid network access
> and LLM judges, and test the suite itself.

## Milestone 5: Baseline model comparison

Run the core suite with three repetitions per case against:

- Qwen 3.5 2B.
- Qwen 3.5 4B.
- Qwen 3.5 9B with reasoning enabled.
- The same Qwen 3.5 9B GGUF with reasoning disabled.
- Gemma 4 E2B.
- Gemma 4 E4B.
- MiniCPM5 2B.

Use consistent quantization where practical and record exceptions. Reasoning
on/off must use the same Qwen weights and otherwise equivalent inference
settings. Prefer explicit llama.cpp server profiles for reasoning mode rather
than assuming an unrecorded per-request default.

Produce a concise checked-in summary containing capability scores, consistency,
tool efficiency, latency, token use, inference metadata, anomalies, and raw
result locations. Do not collapse everything into one unexplained score.

Acceptance criteria:

- Every comparison is reproducible from recorded metadata.
- Failed or unavailable runs remain visible.
- Results separate correctness from tool discipline and performance.
- The summary recommends a default, a fast option, and a higher-quality option,
  or explicitly states why the data cannot support those choices.

Delegation prompt:

> Read `PLAN.md` and the completed evaluation documentation. Execute Milestone
> 5 against the listed seven model profiles with three repetitions per case.
> Verify and record exact weights, quantization, llama.cpp version, reasoning,
> context, sampling, and hardware settings. Preserve failed runs, analyse
> correctness separately from tool discipline and speed, and commit only a
> concise reproducible summary—not bulky raw output. Do not change Spark merely
> to improve one model's score without documenting and rerunning the baseline.

## Milestone 6: Retrieval utilisation experiment

Before implementing web search or full RAG, determine whether each candidate
model can use supplied evidence. Extend the external-library suite with three
conditions per question:

1. No documentation.
2. Search-selected documentation.
3. An oracle passage known to contain the answer.

Interpret the result as follows:

| Observation | Likely conclusion |
| --- | --- |
| Oracle helps but search does not | Retrieval quality is the bottleneck. |
| Search and oracle help | Retrieval is promising. |
| Oracle does not help | Model evidence utilisation is the bottleneck. |
| Retrieval harms known answers | Context selection or prompting needs work. |

Start with a fixed local corpus so document versions and correct answers remain
stable. Evaluate passage count and passage size conservatively; tiny models
should not receive a large document dump.

Acceptance criteria:

- No-doc, retrieved, and oracle conditions differ only in supplied evidence.
- Golden passages and source versions are auditable.
- The report separates retrieval failure from evidence-utilisation failure.
- The findings make an explicit go/no-go recommendation for local retrieval and
  web search.

Delegation prompt:

> Inspect the baseline suite/results and implement Milestone 6 from `PLAN.md`.
> Add a controlled local-document experiment with no-document,
> search-selected, and oracle-passage conditions for versioned Go-library
> questions. Keep prompts and inference settings equivalent, use small bounded
> passages, score correctness and distraction, and report whether failures come
> from retrieval or model evidence utilisation. Do not add production web or
> vector infrastructure yet.

## Milestone 7: Local documentation retrieval prototype

Proceed only if Milestone 6 shows that one or more target models benefit from
good evidence.

Develop in increasing order of complexity:

1. Explicit read-only documentation roots configured by the user.
2. Bounded lexical search over headings, package names, symbols, and text.
3. Query-relevant passages returned with source path, version, and location.
4. Optional full-text/BM25-style indexing if direct search is insufficient.
5. Embeddings and hybrid/vector retrieval only if measured misses justify them.

Do not silently widen the filesystem boundary. Documentation roots must be
explicit, resolved against symlinks, and treated as untrusted data. Indexing
must have a documented refresh lifecycle and bounded storage.

Acceptance criteria:

- The prototype improves the retrieval suite over its no-document baseline.
- Results carry enough provenance for the answer to cite the source.
- Irrelevant context and prompt size are bounded.
- Filesystem and symlink escape tests cover every configured root.
- The report states whether lexical retrieval is sufficient before proposing
  embeddings or a vector database.

Delegation prompt:

> Confirm from the Milestone 6 report that local retrieval has a go decision,
> then implement Milestone 7 from `PLAN.md`. Add explicit read-only doc roots
> and a bounded lexical documentation search that returns small, attributable
> passages. Preserve Spark's workspace security model, add symlink/boundary and
> refresh tests, rerun the retrieval suite, and do not add embeddings unless the
> measured lexical misses justify them.

## Milestone 8: Web search spike

Proceed only if the retrieval experiment shows that models can use evidence and
there are important questions a versioned local corpus cannot answer.

Prototype Kagi-backed search behind an explicit configuration/feature flag.
Prefer canonical documentation and primary sources. Search snippets alone are
not sufficient: fetch, clean, bound, and select relevant passages before giving
them to the model. Return title, URL, retrieval time, and selected text. Treat
all remote content as untrusted and visibly delimit it from Spark instructions.

The spike must address:

- API credentials from environment variables only.
- HTTPS and redirect policy.
- Content-type and response-size limits.
- Timeouts, rate limits, caching, and deterministic test doubles.
- Domain allow/preference controls.
- Prompt injection and misleading/stale source handling.
- Citation/provenance in final answers.

Acceptance criteria:

- The feature is off by default and does not weaken local read boundaries.
- Unit/integration tests do not require a live Kagi account.
- The same retrieval suite demonstrates measurable benefit over no search.
- A short decision record compares web search with maintained local docs and
  recommends whether the feature should graduate from a spike.

Delegation prompt:

> Confirm the retrieval report supports a web-search experiment, then implement
> Milestone 8 from `PLAN.md` as a feature-flagged Kagi spike. Fetch and clean a
> very small number of relevant primary-source passages with strict network,
> size, timeout, credential, provenance, and prompt-injection controls. Use test
> doubles rather than live credentials, rerun the retrieval suite, and write a
> go/no-go decision record. Preserve Spark's read-only design.

## Completion order

- [x] 1. Single-shot CLI
- [x] 2. Revision-range Git diff
- [ ] 3. Evaluation runner and result format
- [ ] 4. Core evaluation suite
- [ ] 5. Baseline model comparison
- [ ] 6. Retrieval utilisation experiment
- [ ] 7. Local documentation retrieval prototype, if justified
- [ ] 8. Web search spike, if justified
