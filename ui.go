package main

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Column indices
const (
	colOverdue  = 0
	colToday    = 1
	colUpcoming = 2
)

// Input modes
const (
	modeNormal = iota
	modeNewTask
	modeEditTask
	modeFilter
	modeHelp
)

type Model struct {
	cfg       Config
	allTasks  []Task
	columns   [3][]int // indices into allTasks
	col       int      // active column
	cursor    [3]int   // cursor position per column
	mode      int
	input     textinput.Model
	filter    string
	width     int
	height    int
	err       error
	statusMsg string
}

// tagColors returns a consistent color for a given tag string.
func tagColor(tag string) lipgloss.Color {
	h := fnv.New32a()
	h.Write([]byte(tag))
	colors := []string{
		"#E06C75", "#98C379", "#E5C07B", "#61AFEF",
		"#C678DD", "#56B6C2", "#D19A66", "#BE5046",
	}
	return lipgloss.Color(colors[h.Sum32()%uint32(len(colors))])
}

func NewModel(cfg Config, tasks []Task) Model {
	ti := textinput.New()
	ti.Placeholder = "Task description #tag 📅 2026-03-01"
	ti.CharLimit = 256
	ti.Width = 60

	m := Model{
		cfg:      cfg,
		allTasks: tasks,
		mode:     modeNormal,
		input:    ti,
	}
	m.categorize()
	return m
}

func (m *Model) categorize() {
	m.columns = [3][]int{}
	today := time.Now().Truncate(24 * time.Hour)

	for i, t := range m.allTasks {
		// Apply filter
		if m.filter != "" {
			low := strings.ToLower(m.filter)
			match := strings.Contains(strings.ToLower(t.Description), low)
			for _, tag := range t.Tags {
				if strings.Contains(strings.ToLower(tag), low) {
					match = true
				}
			}
			if !match {
				continue
			}
		}

		due := t.DueDate.Truncate(24 * time.Hour)
		if due.Before(today) {
			m.columns[colOverdue] = append(m.columns[colOverdue], i)
		} else if due.Equal(today) {
			m.columns[colToday] = append(m.columns[colToday], i)
		} else {
			m.columns[colUpcoming] = append(m.columns[colUpcoming], i)
		}
	}

	// Clamp cursors
	for c := 0; c < 3; c++ {
		if m.cursor[c] >= len(m.columns[c]) {
			m.cursor[c] = max(0, len(m.columns[c])-1)
		}
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Handle input modes first
		if m.mode == modeNewTask || m.mode == modeEditTask || m.mode == modeFilter {
			return m.handleInputMode(msg)
		}
		if m.mode == modeHelp {
			m.mode = modeNormal
			return m, nil
		}
		return m.handleNormalMode(msg)
	}

	return m, nil
}

func (m Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil

	case "enter":
		value := m.input.Value()
		m.input.SetValue("")
		m.input.Blur()

		switch m.mode {
		case modeNewTask:
			m.mode = modeNormal
			if value == "" {
				return m, nil
			}
			// Parse date from input or default to today
			dueDate := time.Now().Truncate(24 * time.Hour)
			if dm := dueDateRe.FindStringSubmatch(value); dm != nil {
				if t, err := time.Parse("2006-01-02", dm[1]); err == nil {
					dueDate = t
				}
				// Remove date from description for CreateTask (it adds its own)
				value = dueDateRe.ReplaceAllString(value, "")
				value = strings.TrimSpace(value)
			}
			if err := CreateTask(m.cfg, value, dueDate); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = "Task created!"
				m = m.reload()
			}

		case modeEditTask:
			m.mode = modeNormal
			if value == "" {
				return m, nil
			}
			task := m.selectedTask()
			if task == nil {
				return m, nil
			}
			// Rebuild the line with the new description
			newLine := "- "
			if task.Done {
				newLine += "[x] "
			} else {
				newLine += "[ ] "
			}
			newLine += value
			for _, tag := range task.Tags {
				newLine += " " + tag
			}
			if !task.DueDate.IsZero() {
				newLine += " 📅 " + task.DueDate.Format("2006-01-02")
			}
			if !task.CompletionDate.IsZero() {
				newLine += " ✅ " + task.CompletionDate.Format("2006-01-02")
			}
			if err := UpdateTaskLine(task, newLine); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = "Task updated!"
				m = m.reload()
			}

		case modeFilter:
			m.mode = modeNormal
			m.filter = value
			m.categorize()
		}
		return m, nil
	}

	// Forward to text input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		if len(m.columns[m.col]) > 0 {
			m.cursor[m.col] = min(m.cursor[m.col]+1, len(m.columns[m.col])-1)
		}

	case "k", "up":
		if len(m.columns[m.col]) > 0 {
			m.cursor[m.col] = max(m.cursor[m.col]-1, 0)
		}

	case "h", "left":
		m.col = max(m.col-1, 0)

	case "l", "right":
		m.col = min(m.col+1, 2)

	case "enter", "d":
		task := m.selectedTask()
		if task != nil {
			if err := ToggleDone(task); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				if task.Done {
					m.statusMsg = "Marked done!"
				} else {
					m.statusMsg = "Marked undone!"
				}
				m.categorize()
			}
		}

	case "D":
		task := m.selectedTask()
		if task != nil {
			if err := DeleteTask(task); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = "Task deleted!"
				m = m.reload()
			}
		}

	case "n":
		m.mode = modeNewTask
		m.input.Placeholder = "Task description #tag 📅 2026-03-01"
		m.input.SetValue("")
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()

	case "e":
		task := m.selectedTask()
		if task != nil {
			m.mode = modeEditTask
			m.input.Placeholder = "Edit task description"
			m.input.SetValue(task.Description)
			m.input.Focus()
			return m, m.input.Cursor.BlinkCmd()
		}

	case "/":
		m.mode = modeFilter
		m.input.Placeholder = "Filter tasks..."
		m.input.SetValue(m.filter)
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()

	case "escape", "esc":
		if m.filter != "" {
			m.filter = ""
			m.categorize()
		}

	case "?":
		m.mode = modeHelp

	case "r":
		m = m.reload()
		m.statusMsg = "Reloaded!"
	}

	return m, nil
}

func (m Model) selectedTask() *Task {
	col := m.columns[m.col]
	if len(col) == 0 {
		return nil
	}
	idx := col[m.cursor[m.col]]
	return &m.allTasks[idx]
}

func (m Model) reload() Model {
	tasks, err := ScanDailyNotes(m.cfg)
	if err != nil {
		m.err = err
		m.statusMsg = "Reload error: " + err.Error()
		return m
	}
	m.allTasks = tasks
	m.categorize()
	return m
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.mode == modeHelp {
		return m.renderHelp()
	}

	colWidth := (m.width - 4) / 3
	if colWidth < 20 {
		colWidth = 20
	}

	// Available height for task items (reserve lines for header, footer, borders)
	availHeight := m.height - 6

	overdueCol := m.renderColumn("Overdue", colOverdue, colWidth, availHeight,
		lipgloss.Color(m.cfg.Theme.Overdue))
	todayCol := m.renderColumn("Today", colToday, colWidth, availHeight,
		lipgloss.Color(m.cfg.Theme.Today))
	upcomingCol := m.renderColumn("Upcoming", colUpcoming, colWidth, availHeight,
		lipgloss.Color(m.cfg.Theme.Upcoming))

	board := lipgloss.JoinHorizontal(lipgloss.Top, overdueCol, todayCol, upcomingCol)

	// Status bar
	status := m.renderStatusBar()

	// Input area
	var inputArea string
	if m.mode == modeNewTask || m.mode == modeEditTask || m.mode == modeFilter {
		prefix := "New: "
		if m.mode == modeEditTask {
			prefix = "Edit: "
		} else if m.mode == modeFilter {
			prefix = "Filter: "
		}
		inputArea = "\n" + prefix + m.input.View()
	}

	return board + "\n" + status + inputArea
}

func (m Model) renderColumn(title string, colIdx int, width int, height int, accent lipgloss.Color) string {
	items := m.columns[colIdx]
	isActive := m.col == colIdx

	// Title style
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(accent).
		PaddingLeft(1)

	headerText := fmt.Sprintf("%s (%d)", title, len(items))
	header := titleStyle.Render(headerText)

	// Render items
	var rows []string
	for i, taskIdx := range items {
		task := m.allTasks[taskIdx]
		row := m.renderTask(task, isActive && i == m.cursor[colIdx], colIdx, width-4)
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true).
			PaddingLeft(2)
		rows = append(rows, emptyStyle.Render("no tasks"))
	}

	content := header + "\n" + strings.Join(rows, "\n")

	// Column border style
	borderColor := lipgloss.Color("#444444")
	if isActive {
		borderColor = accent
	}

	colStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(width).
		Height(height)

	return colStyle.Render(content)
}

func (m Model) renderTask(task Task, selected bool, colIdx int, maxWidth int) string {
	// Bullet
	bullet := "○"
	if task.Done {
		bullet = "✓"
	}

	// Description
	desc := task.Description
	if len(desc) > maxWidth-6 {
		desc = desc[:maxWidth-9] + "..."
	}

	// Tags
	var tagStr string
	for _, tag := range task.Tags {
		tagStyle := lipgloss.NewStyle().
			Foreground(tagColor(tag))
		tagStr += " " + tagStyle.Render(tag)
	}

	// Date suffix for upcoming
	var dateSuffix string
	if colIdx == colUpcoming && !task.DueDate.IsZero() {
		dateSuffix = " " + task.DueDate.Format("Jan 02")
	}

	// Build line
	line := fmt.Sprintf("  %s %s%s%s", bullet, desc, tagStr, dateSuffix)

	style := lipgloss.NewStyle()

	if task.Done {
		style = style.
			Foreground(lipgloss.Color(m.cfg.Theme.Done)).
			Strikethrough(true)
	}

	if selected {
		style = style.
			Background(lipgloss.Color("#333333")).
			Bold(true)
	}

	return style.Render(line)
}

func (m Model) renderStatusBar() string {
	var left string
	if m.statusMsg != "" {
		left = m.statusMsg
	}

	keys := " [n]New  [e]Edit  [d]Done  [D]Del  [/]Filter  [?]Help  [q]Quit"

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	if left != "" {
		accentStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Bold(true)
		return accentStyle.Render(left) + statusStyle.Render(keys)
	}

	return statusStyle.Render(keys)
}

func (m Model) renderHelp() string {
	helpText := `
  Obsidian Tasks TUI — Help

  Navigation:
    j/k or ↑/↓     Move up/down in column
    h/l or ←/→     Switch columns

  Actions:
    Enter or d      Toggle done/undone
    n               New task
    e               Edit task description
    D               Delete task
    /               Filter by text
    r               Reload tasks
    Esc             Clear filter

  Press any key to close help.
`
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7571F9")).
		Padding(1, 3).
		Width(50)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		style.Render(helpText))
}
