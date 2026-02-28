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

// Pane focus
const (
	paneDate = iota
	paneTask
)

// Input modes
const (
	modeNormal = iota
	modeNewTask
	modeEditTask
	modeFilter
	modeHelp
	modeConfirmDelete
)

// DateGroup represents a group of tasks under a date heading.
type DateGroup struct {
	Date     time.Time
	Label    string
	Category int // 0=overdue, 1=today, 2=upcoming
	Tasks    []int
}

const (
	catOverdue  = 0
	catToday    = 1
	catUpcoming = 2
)

type Model struct {
	cfg        Config
	allTasks   []Task
	groups     []DateGroup
	pane       int // paneDate or paneTask
	dateCursor int
	taskCursor int
	mode       int
	input      textinput.Model
	filter     string
	width      int
	height     int
	err        error
	statusMsg  string
	statusTime time.Time
}

// tagColor returns a consistent color for a given tag string.
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
	ti.Placeholder = "Task description #tag"
	ti.CharLimit = 256
	ti.Width = 50

	m := Model{
		cfg:      cfg,
		allTasks: tasks,
		mode:     modeNormal,
		input:    ti,
		pane:     paneDate,
	}
	m.buildGroups()
	return m
}

func (m *Model) buildGroups() {
	m.groups = nil
	today := time.Now().Truncate(24 * time.Hour)

	// Collect tasks by date
	dateMap := make(map[string][]int)
	dateObj := make(map[string]time.Time)

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
		key := due.Format("2006-01-02")
		dateMap[key] = append(dateMap[key], i)
		dateObj[key] = due
	}

	// Sort dates and group
	type dateEntry struct {
		date  time.Time
		key   string
		tasks []int
	}

	var overdue, todayGroup, upcoming []dateEntry

	for key, tasks := range dateMap {
		d := dateObj[key]
		entry := dateEntry{date: d, key: key, tasks: tasks}
		if d.Before(today) {
			overdue = append(overdue, entry)
		} else if d.Equal(today) {
			todayGroup = append(todayGroup, entry)
		} else {
			upcoming = append(upcoming, entry)
		}
	}

	// Sort each group by date
	sortEntries := func(entries []dateEntry) {
		for i := 0; i < len(entries); i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].date.Before(entries[i].date) {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
	}
	sortEntries(overdue)
	sortEntries(todayGroup)
	sortEntries(upcoming)

	// Build groups with section headers
	addGroup := func(entries []dateEntry, cat int) {
		for _, e := range entries {
			label := e.date.Format("Jan 02, Mon")
			if cat == catToday {
				label = "Today - " + e.date.Format("Jan 02")
			}
			m.groups = append(m.groups, DateGroup{
				Date:     e.date,
				Label:    label,
				Category: cat,
				Tasks:    e.tasks,
			})
		}
	}

	addGroup(overdue, catOverdue)
	addGroup(todayGroup, catToday)
	addGroup(upcoming, catUpcoming)

	// Clamp cursors
	if m.dateCursor >= len(m.groups) {
		m.dateCursor = max(0, len(m.groups)-1)
	}
	if len(m.groups) > 0 {
		tasks := m.groups[m.dateCursor].Tasks
		if m.taskCursor >= len(tasks) {
			m.taskCursor = max(0, len(tasks)-1)
		}
	} else {
		m.taskCursor = 0
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
		if m.mode == modeNewTask || m.mode == modeEditTask || m.mode == modeFilter {
			return m.handleInputMode(msg)
		}
		if m.mode == modeHelp {
			m.mode = modeNormal
			return m, nil
		}
		if m.mode == modeConfirmDelete {
			return m.handleConfirmDelete(msg)
		}
		return m.handleNormalMode(msg)
	}

	return m, nil
}

func (m Model) handleConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		task := m.selectedTask()
		if task != nil {
			if err := DeleteTask(task); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = "Task deleted"
				m.statusTime = time.Now()
				m = m.reload()
			}
		}
		m.mode = modeNormal
	case "n", "N", "esc", "q":
		m.mode = modeNormal
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
			dueDate := time.Now().Truncate(24 * time.Hour)
			// Use selected date if on date pane
			if len(m.groups) > 0 && m.dateCursor < len(m.groups) {
				dueDate = m.groups[m.dateCursor].Date
			}
			if dm := dueDateRe.FindStringSubmatch(value); dm != nil {
				if t, err := time.Parse("2006-01-02", dm[1]); err == nil {
					dueDate = t
				}
				value = dueDateRe.ReplaceAllString(value, "")
				value = strings.TrimSpace(value)
			}
			if err := CreateTask(m.cfg, value, dueDate); err != nil {
				m.err = err
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = "Task created"
				m.statusTime = time.Now()
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
				m.statusMsg = "Task updated"
				m.statusTime = time.Now()
				m = m.reload()
			}

		case modeFilter:
			m.mode = modeNormal
			m.filter = value
			m.buildGroups()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab", "h", "l":
		if m.pane == paneDate {
			m.pane = paneTask
		} else {
			m.pane = paneDate
		}

	case "j", "down":
		if m.pane == paneDate {
			if len(m.groups) > 0 {
				m.dateCursor = min(m.dateCursor+1, len(m.groups)-1)
				m.taskCursor = 0
			}
		} else {
			if len(m.groups) > 0 && m.dateCursor < len(m.groups) {
				tasks := m.groups[m.dateCursor].Tasks
				if len(tasks) > 0 {
					m.taskCursor = min(m.taskCursor+1, len(tasks)-1)
				}
			}
		}

	case "k", "up":
		if m.pane == paneDate {
			if len(m.groups) > 0 {
				m.dateCursor = max(m.dateCursor-1, 0)
				m.taskCursor = 0
			}
		} else {
			if len(m.groups) > 0 {
				m.taskCursor = max(m.taskCursor-1, 0)
			}
		}

	case "enter", "d":
		if m.pane == paneTask {
			task := m.selectedTask()
			if task != nil {
				if err := ToggleDone(task); err != nil {
					m.err = err
					m.statusMsg = "Error: " + err.Error()
				} else {
					if task.Done {
						m.statusMsg = "Marked done"
					} else {
						m.statusMsg = "Marked undone"
					}
					m.statusTime = time.Now()
					m.buildGroups()
				}
			}
		} else if m.pane == paneDate {
			// Switch to task pane on enter
			m.pane = paneTask
			m.taskCursor = 0
		}

	case "D":
		if m.pane == paneTask {
			task := m.selectedTask()
			if task != nil {
				m.mode = modeConfirmDelete
			}
		}

	case "n":
		m.mode = modeNewTask
		m.input.Placeholder = "Task description #tag"
		m.input.SetValue("")
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()

	case "e":
		if m.pane == paneTask {
			task := m.selectedTask()
			if task != nil {
				m.mode = modeEditTask
				m.input.Placeholder = "Edit description"
				m.input.SetValue(task.Description)
				m.input.Focus()
				return m, m.input.Cursor.BlinkCmd()
			}
		}

	case "/":
		m.mode = modeFilter
		m.input.Placeholder = "Filter tasks..."
		m.input.SetValue(m.filter)
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()

	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.buildGroups()
		}

	case "?":
		m.mode = modeHelp

	case "r":
		m = m.reload()
		m.statusMsg = "Reloaded"
		m.statusTime = time.Now()
	}

	return m, nil
}

func (m Model) selectedTask() *Task {
	if len(m.groups) == 0 || m.dateCursor >= len(m.groups) {
		return nil
	}
	tasks := m.groups[m.dateCursor].Tasks
	if len(tasks) == 0 || m.taskCursor >= len(tasks) {
		return nil
	}
	idx := tasks[m.taskCursor]
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
	m.buildGroups()
	return m
}

// ── Styles ──────────────────────────────────────────────────

var (
	appStyle = lipgloss.NewStyle().Padding(1, 2)

	subtleBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
	}
)

// ── View ────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.mode == modeHelp {
		return m.renderHelp()
	}

	// Layout dimensions
	totalWidth := m.width - 4
	dateWidth := totalWidth * 28 / 100
	if dateWidth < 22 {
		dateWidth = 22
	}
	taskWidth := totalWidth - dateWidth - 1
	contentHeight := m.height - 5

	// Render panes
	datePane := m.renderDatePane(dateWidth, contentHeight)
	taskPane := m.renderTaskPane(taskWidth, contentHeight)

	board := lipgloss.JoinHorizontal(lipgloss.Top, datePane, taskPane)

	// Footer
	footer := m.renderFooter(totalWidth)

	// Input area
	var inputArea string
	if m.mode == modeNewTask || m.mode == modeEditTask || m.mode == modeFilter {
		prefix := " New: "
		if m.mode == modeEditTask {
			prefix = " Edit: "
		} else if m.mode == modeFilter {
			prefix = " Filter: "
		}
		prefixStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Bold(true)
		inputArea = "\n" + prefixStyle.Render(prefix) + m.input.View()
	}

	if m.mode == modeConfirmDelete {
		confirmStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Overdue)).
			Bold(true)
		inputArea = "\n" + confirmStyle.Render(" Delete this task? [y/n]")
	}

	return board + "\n" + footer + inputArea
}

func (m Model) renderDatePane(width, height int) string {
	isActive := m.pane == paneDate

	accent := lipgloss.Color(m.cfg.Theme.Accent)
	borderColor := lipgloss.Color("#3a3a3a")
	if isActive {
		borderColor = accent
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1a1a2e")).
		Background(accent).
		Padding(0, 1).
		Bold(true)

	title := titleStyle.Render("Dates")

	var rows []string
	lastCat := -1

	for i, g := range m.groups {
		// Section header
		if g.Category != lastCat {
			lastCat = g.Category
			var headerText string
			var headerColor lipgloss.Color
			switch g.Category {
			case catOverdue:
				headerText = "  OVERDUE"
				headerColor = lipgloss.Color(m.cfg.Theme.Overdue)
			case catToday:
				headerText = "  TODAY"
				headerColor = lipgloss.Color(m.cfg.Theme.Today)
			case catUpcoming:
				headerText = "  UPCOMING"
				headerColor = lipgloss.Color(m.cfg.Theme.Upcoming)
			}
			headerStyle := lipgloss.NewStyle().
				Foreground(headerColor).
				Bold(true)
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, headerStyle.Render(headerText))
		}

		// Date entry
		selected := i == m.dateCursor
		taskCount := len(g.Tasks)

		label := fmt.Sprintf("  %s (%d)", g.Label, taskCount)

		style := lipgloss.NewStyle().Width(width - 4)

		switch g.Category {
		case catOverdue:
			style = style.Foreground(lipgloss.Color(m.cfg.Theme.Overdue))
		case catToday:
			style = style.Foreground(lipgloss.Color(m.cfg.Theme.Today)).Bold(true)
		case catUpcoming:
			style = style.Foreground(lipgloss.Color("#999999"))
		}

		if selected && isActive {
			style = style.
				Background(lipgloss.Color("#2a2a3a")).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(accent)
		} else if selected {
			style = style.
				Background(lipgloss.Color("#1e1e2e"))
			label = " " + label
		} else {
			label = " " + label
		}

		rows = append(rows, style.Render(label))
	}

	if len(m.groups) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true).
			Padding(1, 2)
		rows = append(rows, emptyStyle.Render("no dates found"))
	}

	// Scrolling: if there are too many rows, show a window
	content := strings.Join(rows, "\n")

	paneStyle := lipgloss.NewStyle().
		Border(subtleBorder).
		BorderForeground(borderColor).
		Width(width).
		Height(height)

	return paneStyle.Render(title + "\n\n" + content)
}

func (m Model) renderTaskPane(width, height int) string {
	isActive := m.pane == paneTask

	accent := lipgloss.Color(m.cfg.Theme.Accent)
	borderColor := lipgloss.Color("#3a3a3a")
	if isActive {
		borderColor = accent
	}

	// Title shows selected date
	var titleText string
	if len(m.groups) > 0 && m.dateCursor < len(m.groups) {
		g := m.groups[m.dateCursor]
		titleText = "Tasks - " + g.Label
	} else {
		titleText = "Tasks"
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1a1a2e")).
		Background(accent).
		Padding(0, 1).
		Bold(true)

	title := titleStyle.Render(titleText)

	var rows []string
	if len(m.groups) > 0 && m.dateCursor < len(m.groups) {
		tasks := m.groups[m.dateCursor].Tasks
		for i, taskIdx := range tasks {
			task := m.allTasks[taskIdx]
			selected := i == m.taskCursor && isActive
			row := m.renderTaskRow(task, selected, width-4)
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true).
			Padding(1, 2)
		rows = append(rows, emptyStyle.Render("no tasks"))
	}

	content := strings.Join(rows, "\n")

	paneStyle := lipgloss.NewStyle().
		Border(subtleBorder).
		BorderForeground(borderColor).
		Width(width).
		Height(height)

	return paneStyle.Render(title + "\n\n" + content)
}

func (m Model) renderTaskRow(task Task, selected bool, maxWidth int) string {
	// Bullet
	bullet := "○"
	bulletColor := lipgloss.Color("#888888")
	if task.Done {
		bullet = "●"
		bulletColor = lipgloss.Color(m.cfg.Theme.Done)
	}

	bulletStyle := lipgloss.NewStyle().
		Foreground(bulletColor).
		PaddingLeft(2)

	// Description
	desc := task.Description
	descMaxWidth := maxWidth - 8
	if descMaxWidth < 10 {
		descMaxWidth = 10
	}
	if len(desc) > descMaxWidth {
		desc = desc[:descMaxWidth-3] + "..."
	}

	descStyle := lipgloss.NewStyle().PaddingLeft(1)
	if task.Done {
		descStyle = descStyle.
			Foreground(lipgloss.Color(m.cfg.Theme.Done)).
			Strikethrough(true)
	}

	// Tags
	var tagParts []string
	for _, tag := range task.Tags {
		tStyle := lipgloss.NewStyle().
			Foreground(tagColor(tag))
		tagParts = append(tagParts, tStyle.Render(tag))
	}
	tagStr := strings.Join(tagParts, " ")

	line := bulletStyle.Render(bullet) + descStyle.Render(desc)
	if tagStr != "" {
		line += " " + tagStr
	}

	rowStyle := lipgloss.NewStyle().Width(maxWidth)

	if selected {
		rowStyle = rowStyle.
			Background(lipgloss.Color("#2a2a3a")).
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color(m.cfg.Theme.Accent))
	}

	return rowStyle.Render(line)
}

func (m Model) renderFooter(width int) string {
	// Status message (auto-clear after 3 seconds)
	var statusPart string
	if m.statusMsg != "" && time.Since(m.statusTime) < 3*time.Second {
		statusStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Bold(true)
		statusPart = statusStyle.Render(" "+m.statusMsg) + "  "
	}

	// Key hints
	keys := "n new  d done  e edit  D del  / filter  ? help  q quit"

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	filterInfo := ""
	if m.filter != "" {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Italic(true)
		filterInfo = filterStyle.Render(" [filter: "+m.filter+"] ")
	}

	return statusPart + filterInfo + keyStyle.Render(keys)
}

func (m Model) renderHelp() string {
	helpText := `
  Obsidian Tasks TUI

  Navigation
    j/k  ↑/↓       Move up/down
    Tab  h/l        Switch panes
    Enter           Select date / toggle done

  Actions
    n               New task
    e               Edit task
    d               Toggle done/undone
    D               Delete task
    /               Filter by text
    r               Reload from files
    Esc             Clear filter

  Press any key to close.
`
	style := lipgloss.NewStyle().
		Border(subtleBorder).
		BorderForeground(lipgloss.Color(m.cfg.Theme.Accent)).
		Padding(1, 3).
		Width(48)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		style.Render(helpText))
}
