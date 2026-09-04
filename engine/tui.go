package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tuiModel struct {
	db      *sql.DB
	cfg     Config
	cards   []Card
	cursor  int
	offset  int
	height  int
	mode    string // day|week|month|search
	query   string
	searching bool
	status  string
	err     error
}

var (
	tuiAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	tuiDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiBold    = lipgloss.NewStyle().Bold(true)
	tuiSel     = lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15"))
	tuiCat     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

func (m tuiModel) loadRange() (tuiModel, tea.Cmd) {
	now := time.Now()
	var start, end time.Time
	switch m.mode {
	case "week":
		start, end = weekBounds(now)
	case "month":
		start, end = monthBounds(now)
	case "search":
		// last 30 days
		start = now.AddDate(0, 0, -30)
		end = now.AddDate(0, 0, 1)
	default:
		start, end = dayBounds(now)
	}
	blocks, err := blocksBetween(m.db, start, end)
	m.err = err
	if m.mode == "search" && m.query != "" {
		var filtered []Block
		q := strings.ToLower(m.query)
		for _, b := range blocks {
			if strings.Contains(strings.ToLower(b.Title+b.Summary+b.App), q) {
				filtered = append(filtered, b)
			}
		}
		blocks = filtered
	}
	m.cards = mergeCards(blocks)
	m.cursor = 0
	m.offset = 0
	return m, nil
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				m.mode = "search"
				return m.loadRange()
			case "esc":
				m.searching = false
				m.query = ""
				return m, nil
			case "backspace":
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				}
			default:
				if len(msg.Runes) > 0 {
					m.query += string(msg.Runes)
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.cards)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "d":
			m.mode = "day"
			return m.loadRange()
		case "w":
			m.mode = "week"
			return m.loadRange()
		case "M":
			m.mode = "month"
			return m.loadRange()
		case "/":
			m.searching = true
			m.query = ""
		case "r":
			return m.loadRange()
		}
	}
	// keep cursor visible
	vis := m.height - 8
	if vis < 3 {
		vis = 3
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	return m, nil
}

func (m tuiModel) View() string {
	var b strings.Builder
	header := fmt.Sprintf(" dayflow — %s ", strings.ToUpper(m.mode))
	b.WriteString(tuiAccent.Render(header))
	if m.searching {
		b.WriteString(tuiDim.Render("  search: ") + m.query + "▌")
	} else if m.mode == "search" {
		b.WriteString(tuiDim.Render("  query: ") + m.query)
	}
	b.WriteString("\n")
	b.WriteString(tuiDim.Render(" j/k move · d day · w week · M month · / search · r refresh · q quit"))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString("  " + m.err.Error() + "\n")
	}
	if len(m.cards) == 0 {
		b.WriteString(tuiDim.Render("  (no activity cards in this range)\n"))
	}
	vis := m.height - 8
	if vis < 3 {
		vis = 3
	}
	end := m.offset + vis
	if end > len(m.cards) {
		end = len(m.cards)
	}
	for i := m.offset; i < end; i++ {
		c := m.cards[i]
		marker := "  "
		style := lipgloss.NewStyle()
		if i == m.cursor {
			marker = "▸ "
			style = tuiSel
		}
		app := c.App
		if app == "" {
			app = "·"
		}
		line := fmt.Sprintf("%s%s–%s  %-16s %s  [%s]", marker, c.StartStr, c.EndStr,
			"@"+truncate(app, 14), c.Title, c.Category)
		b.WriteString(style.Render(line) + "\n")
		if i == m.cursor {
			for _, l := range wrapText(c.Summary, 90) {
				b.WriteString(tuiDim.Render("      "+l) + "\n")
			}
		}
	}
	b.WriteString("\n" + tuiDim.Render(fmt.Sprintf(" %d cards · dayflow %s", len(m.cards), version)))
	return b.String()
}

func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		if len(cur)+len(w)+1 > width {
			lines = append(lines, cur)
			cur = w
		} else if cur == "" {
			cur = w
		} else {
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func runTUI(cfg Config) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	m := tuiModel{db: db, cfg: cfg, mode: "day", height: 24}
	m, _ = m.loadRange()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
