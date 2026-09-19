package commands

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/list"
	"github.com/mikepjb/spark/internal/repl"
	"github.com/mikepjb/spark/internal/skills"
)

type ModelOption struct {
	Name     string
	Provider string
	Model    string
}

type Candidate struct {
	Text        string
	Label       string
	Description string
}

type Token struct {
	Start   int
	End     int
	Trigger rune
	Query   string
	Kind    string
}

type Completion struct {
	Token Token
	Items []Candidate
}

type Prepared struct {
	Submission *repl.Submission
	Local      string
	ModelName  string
}

type Engine struct {
	skills      *skills.Catalog
	models      []ModelOption
	files       []string
	selectModel func(string) (string, error)
}

func New(skillCatalog *skills.Catalog, models []ModelOption, files []string, selectModel func(string) (string, error)) *Engine {
	sorted := append([]ModelOption(nil), models...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	return &Engine{skills: skillCatalog, models: sorted, files: append([]string(nil), files...), selectModel: selectModel}
}

func (e *Engine) Candidates(input string, cursor int) Completion {
	token, ok := ActiveToken(input, cursor)
	if !ok {
		return Completion{}
	}
	var candidates []Candidate
	switch token.Kind {
	case "command":
		candidates = e.commandCandidates(token.Query)
	case "skill":
		for _, skill := range e.skillList() {
			candidates = append(candidates, Candidate{Text: "$" + skill.Name, Label: "$" + skill.Name, Description: skill.Description})
		}
		candidates = filterCandidates(candidates, token.Query)
	case "file":
		for _, path := range e.files {
			candidates = append(candidates, Candidate{Text: "@" + path, Label: "@" + path})
		}
		candidates = filterCandidates(candidates, token.Query)
	case "model":
		for _, option := range e.models {
			text := option.Name
			if token.Start == token.End {
				text = " " + text
			}
			candidates = append(candidates, Candidate{Text: text, Label: option.Name, Description: option.Provider + " · " + option.Model})
		}
		candidates = filterCandidates(candidates, token.Query)
	}
	if len(candidates) == 1 && candidates[0].Text == tokenText(input, token) && (token.Kind != "model" || token.Query != "") {
		candidates = nil
	}
	return Completion{Token: token, Items: candidates}
}

func (e *Engine) Prepare(input string) (Prepared, error) {
	display := strings.TrimSpace(input)
	if display == "" {
		return Prepared{}, fmt.Errorf("message cannot be empty")
	}
	fields := strings.Fields(display)
	if len(fields) == 0 {
		return Prepared{}, fmt.Errorf("message cannot be empty")
	}

	switch fields[0] {
	case "/about":
		return Prepared{Local: "Spark is a lightweight, read-only software engineering assistant."}, nil
	case "/commands":
		return Prepared{Local: e.commandList()}, nil
	case "/model":
		if len(fields) < 2 {
			return Prepared{Local: e.modelList()}, nil
		}
		if e.selectModel == nil {
			return Prepared{}, fmt.Errorf("model selection is unavailable")
		}
		modelName, err := e.selectModel(fields[1])
		if err != nil {
			return Prepared{}, err
		}
		return Prepared{Local: "selected model: " + modelName, ModelName: modelName}, nil
	}

	var uses []repl.SkillUse
	spans := skillTokenSpans(display)
	var removed []tokenSpan
	for index, span := range spans {
		name := ""
		if index == 0 && strings.HasPrefix(span.Text, "/") {
			name = strings.TrimPrefix(span.Text, "/")
		} else if strings.HasPrefix(span.Text, "$") {
			name = strings.TrimPrefix(span.Text, "$")
		}
		if name != "" && e.hasSkill(name) {
			use, err := e.loadSkill(name)
			if err != nil {
				return Prepared{}, err
			}
			uses = append(uses, use)
			removed = append(removed, span)
		}
	}
	prompt := removeSpans(display, removed)
	if prompt == "" {
		prompt = display
	}
	return Prepared{Submission: &repl.Submission{Display: display, Prompt: prompt, Skills: uses}}, nil
}

func ActiveToken(input string, cursor int) (Token, bool) {
	runes := []rune(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	start := cursor
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	end := cursor
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	text := string(runes[start:end])
	firstStart, firstEnd := firstToken(runes)
	first := string(runes[firstStart:firstEnd])
	if start == firstStart && strings.HasPrefix(text, "/") {
		query := strings.TrimPrefix(text, "/")
		if first == "/model" && start == firstStart && cursor >= firstEnd {
			return Token{Start: firstEnd, End: firstEnd, Kind: "model"}, true
		}
		return Token{Start: start, End: end, Trigger: '/', Query: query, Kind: "command"}, true
	}
	if first == "/model" && cursor >= firstEnd {
		query := text
		return Token{Start: start, End: end, Query: query, Kind: "model"}, true
	}
	if strings.HasPrefix(text, "$") {
		return Token{Start: start, End: end, Trigger: '$', Query: strings.TrimPrefix(text, "$"), Kind: "skill"}, true
	}
	if strings.HasPrefix(text, "@") {
		return Token{Start: start, End: end, Trigger: '@', Query: strings.TrimPrefix(text, "@"), Kind: "file"}, true
	}
	return Token{}, false
}

func Apply(input string, token Token, candidate Candidate) (string, int) {
	runes := []rune(input)
	insert := []rune(candidate.Text)
	result := make([]rune, 0, len(runes)-token.End+token.Start+len(insert))
	result = append(result, runes[:token.Start]...)
	result = append(result, insert...)
	result = append(result, runes[token.End:]...)
	return string(result), token.Start + len(insert)
}

func (e *Engine) commandCandidates(query string) []Candidate {
	items := []Candidate{
		{Text: "/about", Label: "/about", Description: "about Spark"},
		{Text: "/commands", Label: "/commands", Description: "list commands"},
		{Text: "/model", Label: "/model", Description: "select a model"},
	}
	for _, skill := range e.skillList() {
		items = append(items, Candidate{Text: "/" + skill.Name, Label: "/" + skill.Name, Description: skill.Description})
	}
	return filterCandidates(items, query)
}

func filterCandidates(items []Candidate, query string) []Candidate {
	if query == "" {
		return items
	}
	targets := make([]string, len(items))
	for i, item := range items {
		targets[i] = item.Label
	}
	ranks := list.DefaultFilter(query, targets)
	result := make([]Candidate, 0, len(ranks))
	for _, rank := range ranks {
		result = append(result, items[rank.Index])
	}
	return result
}

func firstToken(runes []rune) (int, int) {
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	end := start
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	return start, end
}

type tokenSpan struct {
	Start int
	End   int
	Text  string
}

func skillTokenSpans(input string) []tokenSpan {
	runes := []rune(input)
	var spans []tokenSpan
	for index := 0; index < len(runes); {
		for index < len(runes) && unicode.IsSpace(runes[index]) {
			index++
		}
		start := index
		for index < len(runes) && !unicode.IsSpace(runes[index]) {
			index++
		}
		if start < index {
			spans = append(spans, tokenSpan{Start: start, End: index, Text: string(runes[start:index])})
		}
	}
	return spans
}

func removeSpans(input string, spans []tokenSpan) string {
	if len(spans) == 0 {
		return strings.TrimSpace(input)
	}
	runes := []rune(input)
	removed := make(map[int]bool, len(spans))
	for _, span := range spans {
		for index := span.Start; index < span.End; index++ {
			removed[index] = true
		}
	}
	result := make([]rune, 0, len(runes))
	for index, r := range runes {
		if !removed[index] {
			result = append(result, r)
		}
	}
	return strings.TrimSpace(string(result))
}

func tokenText(input string, token Token) string {
	runes := []rune(input)
	if token.Start > len(runes) || token.End > len(runes) {
		return ""
	}
	return string(runes[token.Start:token.End])
}

func (e *Engine) skillList() []skills.Skill {
	if e.skills == nil {
		return nil
	}
	return e.skills.List()
}

func (e *Engine) hasSkill(name string) bool {
	for _, skill := range e.skillList() {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func (e *Engine) loadSkill(name string) (repl.SkillUse, error) {
	_, body, err := e.skills.Load(name)
	if err != nil {
		return repl.SkillUse{}, err
	}
	return repl.SkillUse{Name: name, Body: body}, nil
}

func (e *Engine) commandList() string {
	items := []string{"/about — about Spark", "/commands — list commands", "/model — select a model"}
	for _, skill := range e.skillList() {
		items = append(items, "/"+skill.Name+" — "+skill.Description)
	}
	return strings.Join(items, "\n")
}

func (e *Engine) modelList() string {
	if len(e.models) == 0 {
		return "no models configured"
	}
	items := make([]string, 0, len(e.models))
	for _, option := range e.models {
		items = append(items, option.Name+" — "+option.Provider+" · "+option.Model)
	}
	return strings.Join(items, "\n")
}
