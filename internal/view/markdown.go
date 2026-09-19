package view

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
)

type markdownRenderer struct {
	width int
	term  *glamour.TermRenderer
}

func (r *markdownRenderer) render(content string, width int) (string, error) {
	width = atLeastOne(width)
	if r.term == nil || r.width != width {
		style := styles.DarkStyleConfig
		style.Document.StylePrimitive.BlockPrefix = ""
		style.Document.Margin = nil
		term, err := glamour.NewTermRenderer(
			glamour.WithStyles(style),
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
