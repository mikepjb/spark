package view

import "charm.land/glamour/v2"

type markdownRenderer struct {
	width int
	term  *glamour.TermRenderer
}

func (r *markdownRenderer) render(content string, width int) (string, error) {
	width = atLeastOne(width)
	if r.term == nil || r.width != width {
		term, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", err
		}
		r.width = width
		r.term = term
	}

	return r.term.Render(content)
}
