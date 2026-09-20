# Spark

Spark is a lightweight agent harness for software analysis. It is designed to
keep people in the loop and to expose only tightly controlled, read-only tools.
The initial target is a small local model such as a quantized Qwen 2B or 4B,
running comfortably on a consumer laptop.

## New features to be added

- we should probably give the model some additional context like the time of
  day.. their present working directory? and check other harnesses to see what
  they provide.
- context counting no longer seems to work with minicpm5 / llama.cpp v0.4.1 or
  maybe I messed up my llm bash script.
- make the tool call budget visible to the model in tool call metadata
- distinguish between a 'system prompt' and the users agents prompt.. there are
  some internal things to help drive minicpm5 that will be distinct from user
  agents.md
- after the agent has done all the calls/responded we should have a 'worked for
  9m 51s' or however long the time elapsed has taken.
- look at how pi/opencode/codex to their glob/grep/reads to see if there are any
  tricks we can pull to make this more efficient.

## How this might be useful

- Rather than running any command in bash, if this is necessary, the model can
  still prompt the user to enter the command. People can still copy and paste
  but there is at least some level of exposure and learning that you won't get
  with a fully autonomous agent.


## Technology choices

- Go for a small, portable single-binary application.
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) and
  [Bubbles](https://github.com/charmbracelet/bubbles) for the event-driven TUI.
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal layout
  and styling.
- The OpenAI-compatible Chat Completions API for compatibility with both
  OpenAI and llama.cpp.
- [SQLite](https://www.sqlite.org/) for durable sessions, streamed events,
  tool calls, and run history.
- [Glamour](https://github.com/charmbracelet/glamour), backed by
  [Goldmark](https://github.com/yuin/goldmark) and
  [Chroma](https://github.com/alecthomas/chroma), for terminal Markdown and
  code rendering.
- YAML configuration in `.spark.yaml` and `~/.config/spark/config.yaml`.
  Secrets should come from environment variables rather than project files.

## Initial scope

The first tools are expected to cover bounded filesystem inspection and safe
Git operations such as status, diff, log, and show. Every tool will enforce
workspace boundaries, argument validation, output limits, and timeouts.

Spark will not initially provide:

- saving/exporting output to disk, though when a plan is generated or something
  that you want to review later this would be handy!
- arbitrary shell or code execution;
- file writes, edits, or deletes;
- management or downloading of LLM servers/models;
- embeddings, RAG, or MCP integration.

The repository is currently a project scaffold; the architecture above
describes the intended direction rather than implemented functionality.

## Skills

Spark supports explicit Agent Skills. Skills are directories containing a
`SKILL.md` file with YAML front matter and Markdown instructions.

Use a skill at the start of a request with `/skill-name`, or reference it
inline with `$skill-name`:

```text
/analyse inspect the repository's configuration
inspect the repository's configuration using $analyse
```

By default Spark searches these roots in order:

- `.agents/skills`
- `~/.agents/skills`

The `skill_paths` setting in `.sparkrc` replaces these defaults. Skills are
loaded only for the request that explicitly references them with `/skill-name`
or `$skill-name`. Activated skill instructions are included in that request's
user message; they are not automatically selected, persisted as active context,
or used to execute scripts bundled with the skill. Skill instructions cannot
expand Spark's read-only capabilities.

## Configuration

The `.sparkrc` file controls runtime defaults. Spark bounds each user request
with one total tool-call limit:

```yaml
tool_call_limit: 8
```

The environment variable `SPARK_TOOL_CALL_LIMIT` overrides the file setting.
The limit counts calls across all model/tool rounds for one user request. If a
model emits more calls than remain, Spark executes only the remaining calls and
then asks for a final answer without tools.

Spark's internal system prompt is kept separate from user-level guidance. If
present, Spark loads user guidance from `~/.agents/AGENTS.md`, falling back to
`~/.codex/AGENTS.md`. Activated skills and the user request are also included
as user-level content. The old `system_prompt` configuration key is no longer
supported; put personal workflow guidance in the user-level `AGENTS.md` file.

## Target languages

- Go
- Java
- Python
- TypeScript / JavaScript
- Bash (analysis of scripts, not execution)
