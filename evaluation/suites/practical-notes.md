# Practical suite review notes

These seven cases are a manual comparison set. `spark-eval` records answers,
timing, usage, model rounds, and tool calls; it does not score answers yet.
The library cases test whether the model can answer stable API questions without
unnecessary repository inspection. They require no network access during a run.

| Case | Capability and expected checkpoints |
| --- | --- |
| `project-overview` | Read `README.md`; identify Spark as a read-only software-analysis harness, TUI and single-shot CLI, bounded tools, `.sparkrc` model configuration, and the development evaluator. Treat planned retrieval/web work as plans. |
| `configuration-flow` | Trace `config.Load` → `model.New`/`Select` → `bootstrap` → `repl.New` → `runHeadless`/`SubmitSubmission`. The selected provider supplies the endpoint; the model choice supplies the model name. |
| `go-stdlib-json` | Use `json.NewDecoder(r)`, call `DisallowUnknownFields()`, then `Decode(&dst)`. By default `encoding/json` v1 ignores unknown struct fields. [Go documentation](https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields). |
| `python-stdlib-pathlib` | Use `Path(path).read_text(encoding="utf-8")`; it returns `str` and closes the file. [Python 3.12 documentation](https://docs.python.org/3.12/library/pathlib.html#pathlib.Path.read_text). |
| `go-third-party-yaml` | Use `yaml.NewDecoder(r)`, `KnownFields(true)`, then `Decode(&dst)`. Plain `Unmarshal` does not enable the strict-key check. [yaml.v3 documentation](https://pkg.go.dev/gopkg.in/yaml.v3#Decoder.KnownFields). |
| `python-third-party-requests` | Use `requests.get(url, timeout=(connect_seconds, read_seconds))` and `response.raise_for_status()`. The read timeout limits waiting between received bytes, not total download time. [Requests documentation](https://requests.readthedocs.io/en/latest/user/advanced/#timeouts). |
| `code-review-cache` | Find the three seeded defects below, with function names and concrete scenarios. Avoid claiming unrelated defects without evidence. |

## Review fixture defects

- **CACHE-1 (medium):** `Get` ignores `expiresAt`, so an expired entry remains
  visible until `DeleteExpired` runs. A negative TTL reproduces this immediately.
- **CACHE-2 (medium):** `Set` stores the caller's `[]byte`, and `Get` returns the
  stored slice. Mutating either slice changes cached data outside the lock and
  can race with concurrent readers. Copy on insertion and return.
- **CACHE-3 (high):** `DeleteExpired` deletes map entries under `RLock`; another
  reader can hold `RLock` concurrently, so a concurrent `Get` can race with a
  map mutation. Use the exclusive `Lock` for deletion.
