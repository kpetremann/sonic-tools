// Package view renders the device state for humans.
package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	headerStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	cellStyle   = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	ruleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	faintStyle = lipgloss.NewStyle().Faint(true).PaddingLeft(2)
)

// Header returns the title of a section.
func Header(title string) string {
	rule := ruleStyle.Render("──")
	return fmt.Sprintf("%s %s %s\n", rule, titleStyle.Render(title), rule)
}

// Comment returns a dimmed line, used for empty sections and summaries.
func Comment(msg string) string {
	return fmt.Sprintln(faintStyle.Render(msg))
}

type Table struct {
	headers []string
	rows    [][]string
}

func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

func (t *Table) Row(values ...string) {
	row := make([]string, len(values))
	for i, value := range values {
		row[i] = value
		if value == "" {
			row[i] = "-"
		}
	}
	t.rows = append(t.rows, row)
}

func (t *Table) String() string {
	if len(t.headers) == 0 {
		return ""
	}

	lt := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		Headers(t.headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	for _, row := range t.rows {
		lt.Row(row...)
	}

	return lt.String() + "\n"
}
