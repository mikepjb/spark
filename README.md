# Spark

Spark is a lightweight agent harness for software analysis. It is designed to
keep people in the loop and to expose only tightly controlled, read-only tools.
The initial target is a small local model such as a quantized Qwen 2B or 4B,
running comfortably on a consumer laptop.

## Features to be implemented

- /model be able to switch between llama.cpp qwen and openai/fireworks
- make sure we are printing feedback in the UI when tool calls are being used
- display issue, there is no 'in-progress' display in the history for when the
  LLM is inferencing or 'thinking'
- display issue, currently llm output in the history seems to have an empty line
  at the beginning - not sure if that's a parsing issue or what's going on
  there.
- Read/Grep etc combine into a Explored phase visually

## How this might be useful

- Rather than running any command in bash, if this is necessary, the model can
  still prompt the user to enter the command. People can still copy and paste
  but there is at least some level of exposure and learning that you won't get
  with a fully autonomous agent.

## Human Approach

- I want to use the `/v1/chat/completions` API running against llama.cpp that
  has Qwen 3.5 2B loaded.

## Generated Approach

Spark will be a local TUI and agent coordinator that connects to an externally
managed LLM server:

```text
TUI -> agent coordinator -> OpenAI-compatible model API
                       -> structured read-only tools
                       -> SQLite sessions and event log
```

The LLM server, such as [llama.cpp](https://github.com/ggml-org/llama.cpp), is
not managed by Spark. Spark only reads its connection configuration. Model
output is streamed, accumulated as an event history, and rendered as Markdown
while it arrives.

Tools execute automatically, but they are capabilities rather than arbitrary
shell commands. Git operations will be exposed through a dedicated allowlisted
tool. Other useful analysis operations, such as filtering or extracting lines,
will be implemented directly rather than by exposing `bash`, `awk`, or a
generic command runner.

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

## Target languages

- Go
- Java
- Python
- TypeScript / JavaScript
- Bash (analysis of scripts, not execution)
