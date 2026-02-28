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

const (
	viewToday = iota
	viewUpcoming
	viewLogbook
)

const (
	focusSidebar = iota
	focusContent
)

const (
	modeNormal = iota
	modeNewTask
	modeEditTask
	modeFilter
	modeHelp
	modeConfirmDelete
)

type DateGroup struct {
	Date  time.Time
	Label string
	Tasks []int
}

type Model struct {
	cfg      Config
	allTasks []Task

	activeView    int
	focus         int
	sidebarCursor int
	contentCursor int
	scrollOffset  int

	todayTasks     []int
	overdueStart   int
	upcomingGroups []DateGroup
	logbookGroups  []DateGroup

	mode       int
	width      int
	height     int
	input      textinput.Model
	filter     string
	statusMsg  string
	statusTime time.Time
	err        error
}

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
		cfg:        cfg,
		allTasks:   tasks,
		mode:       modeNormal,
		input:      ti,
		activeView: viewToday,
		focus:      focusSidebar,
	}
	m.buildViews()
	return m
}

func (m *Model) matchesFilter(t Task) bool {
	if m.filter == "" {
		return true
	}
	low := strings.ToLower(m.filter)
	if strings.Contains(strings.ToLower(t.Description), low) {
		return true
	}
	for _, tag := range t.Tags {
		if strings.Contains(strings.ToLower(tag), low) {
			return true
		}
	}
	return false
}

func (m *Model) buildViews() {
	today := time.Now().Truncate(24 * time.Hour)

	m.todayTasks = nil
	m.overdueStart = 0
	m.upcomingGroups = nil
	m.logbookGroups = nil

	var todayUndone []int
	var overdueUndone []int
	upcomingMap := make(map[string][]int)
	upcomingDates := make(map[string]time.Time)
	logbookMap := make(map[string][]int)
	logbookDates := make(map[string]time.Time)

	for i, t := range m.allTasks {
		if !m.matchesFilter(t) {
			continue
		}
		due := t.DueDate.Truncate(24 * time.Hour)

		if t.Done {
			compDate := t.CompletionDate.Truncate(24 * time.Hour)
			if compDate.IsZero() {
				compDate = due
			}
			key := compDate.Format("2006-01-02")
			logbookMap[key] = append(logbookMap[key], i)
			logbookDates[key] = compDate
			continue
		}

		if due.After(today) {
			key := due.Format("2006-01-02")
			upcomingMap[key] = append(upcomingMap[key], i)
			upcomingDates[key] = due
		} else if due.Equal(today) {
			todayUndone = append(todayUndone, i)
		} else {
			overdueUndone = append(overdueUndone, i)
		}
	}

	m.todayTasks = append(m.todayTasks, todayUndone...)
	m.overdueStart = len(m.todayTasks)
	sortByDueDate := func(indices []int) {
		for i := 0; i < len(indices); i++ {
			for j := i + 1; j < len(indices); j++ {
				if m.allTasks[indices[j]].DueDate.Before(m.allTasks[indices[i]].DueDate) {
					indices[i], indices[j] = indices[j], indices[i]
				}
			}
		}
	}
	sortByDueDate(overdueUndone)
	m.todayTasks = append(m.todayTasks, overdueUndone...)

	var upcomingSorted []string
	for key := range upcomingMap {
		upcomingSorted = append(upcomingSorted, key)
	}
	for i := 0; i < len(upcomingSorted); i++ {
		for j := i + 1; j < len(upcomingSorted); j++ {
			if upcomingSorted[j] < upcomingSorted[i] {
				upcomingSorted[i], upcomingSorted[j] = upcomingSorted[j], upcomingSorted[i]
			}
		}
	}
	for _, key := range upcomingSorted {
		m.upcomingGroups = append(m.upcomingGroups, DateGroup{
			Date:  upcomingDates[key],
			Label: upcomingDates[key].Format("Mon, Jan 02"),
			Tasks: upcomingMap[key],
		})
	}

	var logbookSorted []string
	for key := range logbookMap {
		logbookSorted = append(logbookSorted, key)
	}
	for i := 0; i < len(logbookSorted); i++ {
		for j := i + 1; j < len(logbookSorted); j++ {
			if logbookSorted[j] > logbookSorted[i] {
				logbookSorted[i], logbookSorted[j] = logbookSorted[j], logbookSorted[i]
			}
		}
	}
	for _, key := range logbookSorted {
		m.logbookGroups = append(m.logbookGroups, DateGroup{
			Date:  logbookDates[key],
			Label: logbookDates[key].Format("Jan 02"),
			Tasks: logbookMap[key],
		})
	}

	m.clampCursor()
}

func (m *Model) currentViewTasks() []int {
	switch m.activeView {
	case viewToday:
		return m.todayTasks
	case viewUpcoming:
		var flat []int
		for _, g := range m.upcomingGroups {
			flat = append(flat, g.Tasks...)
		}
		return flat
	case viewLogbook:
		var flat []int
		for _, g := range m.logbookGroups {
			flat = append(flat, g.Tasks...)
		}
		return flat
	}
	return nil
}

func (m *Model) clampCursor() {
	tasks := m.currentViewTasks()
	if m.contentCursor >= len(tasks) {
		m.contentCursor = max(0, len(tasks)-1)
	}
	if m.sidebarCursor > 2 {
		m.sidebarCursor = 2
	}
}

func (m *Model) viewTaskCount(view int) int {
	switch view {
	case viewToday:
		return len(m.todayTasks)
	case viewUpcoming:
		count := 0
		for _, g := range m.upcomingGroups {
			count += len(g.Tasks)
		}
		return count
	case viewLogbook:
		count := 0
		for _, g := range m.logbookGroups {
			count += len(g.Tasks)
		}
		return count
	}
	return 0
}

func (m Model) selectedTask() *Task {
	tasks := m.currentViewTasks()
	if len(tasks) == 0 || m.contentCursor >= len(tasks) {
		return nil
	}
	return &m.allTasks[tasks[m.contentCursor]]
}

func (m Model) reload() Model {
	tasks, err := ScanDailyNotes(m.cfg)
	if err != nil {
		m.err = err
		m.statusMsg = "Reload error: " + err.Error()
		return m
	}
	m.allTasks = tasks
	m.buildViews()
	return m
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
			if m.activeView == viewUpcoming && len(m.upcomingGroups) > 0 {
				groupIdx := m.groupIndexForCursor()
				if groupIdx >= 0 && groupIdx < len(m.upcomingGroups) {
					dueDate = m.upcomingGroups[groupIdx].Date
				}
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
			m.buildViews()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) groupIndexForCursor() int {
	cursor := m.contentCursor
	var groups []DateGroup
	switch m.activeView {
	case viewUpcoming:
		groups = m.upcomingGroups
	case viewLogbook:
		groups = m.logbookGroups
	default:
		return -1
	}
	offset := 0
	for i, g := range groups {
		if cursor < offset+len(g.Tasks) {
			return i
		}
		offset += len(g.Tasks)
	}
	return len(groups) - 1
}

func (m Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "1":
		m.activeView = viewToday
		m.sidebarCursor = 0
		m.contentCursor = 0
		m.scrollOffset = 0

	case "2":
		m.activeView = viewUpcoming
		m.sidebarCursor = 1
		m.contentCursor = 0
		m.scrollOffset = 0

	case "3":
		m.activeView = viewLogbook
		m.sidebarCursor = 2
		m.contentCursor = 0
		m.scrollOffset = 0

	case "tab":
		if m.focus == focusSidebar {
			m.focus = focusContent
		} else {
			m.focus = focusSidebar
		}

	case "h":
		m.focus = focusSidebar

	case "l":
		if m.focus == focusSidebar {
			m.focus = focusContent
		}

	case "j", "down":
		if m.focus == focusSidebar {
			if m.sidebarCursor < 2 {
				m.sidebarCursor++
				m.activeView = m.sidebarCursor
				m.contentCursor = 0
				m.scrollOffset = 0
			}
		} else {
			tasks := m.currentViewTasks()
			if len(tasks) > 0 && m.contentCursor < len(tasks)-1 {
				m.contentCursor++
			}
		}

	case "k", "up":
		if m.focus == focusSidebar {
			if m.sidebarCursor > 0 {
				m.sidebarCursor--
				m.activeView = m.sidebarCursor
				m.contentCursor = 0
				m.scrollOffset = 0
			}
		} else {
			if m.contentCursor > 0 {
				m.contentCursor--
			}
		}

	case "enter":
		if m.focus == focusSidebar {
			m.focus = focusContent
		} else if m.activeView != viewLogbook {
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
					m = m.reload()
				}
			}
		}

	case "d":
		if m.focus == focusContent && m.activeView != viewLogbook {
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
					m = m.reload()
				}
			}
		}

	case "D":
		if m.focus == focusContent && m.activeView != viewLogbook {
			task := m.selectedTask()
			if task != nil {
				m.mode = modeConfirmDelete
			}
		}

	case "n":
		if m.activeView != viewLogbook {
			m.mode = modeNewTask
			m.input.Placeholder = "Task description #tag"
			m.input.SetValue("")
			m.input.Focus()
			return m, m.input.Cursor.BlinkCmd()
		}

	case "e":
		if m.focus == focusContent && m.activeView != viewLogbook {
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
			m.buildViews()
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

var (
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

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.mode == modeHelp {
		return m.renderHelp()
	}

	totalWidth := m.width - 4
	sidebarWidth := 16
	contentWidth := totalWidth - sidebarWidth - 1
	contentHeight := m.height - 5

	sidebar := m.renderSidebar(sidebarWidth, contentHeight)
	content := m.renderContent(contentWidth, contentHeight)

	board := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, content)

	footer := m.renderFooter(totalWidth)

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

func (m Model) renderSidebar(width, height int) string {
	isActive := m.focus == focusSidebar
	accent := lipgloss.Color(m.cfg.Theme.Accent)
	borderColor := lipgloss.Color("#3a3a3a")
	if isActive {
		borderColor = accent
	}

	type sidebarItem struct {
		icon  string
		label string
		view  int
	}
	items := []sidebarItem{
		{"☀", "Today", viewToday},
		{"📅", "Upcoming", viewUpcoming},
		{"📓", "Logbook", viewLogbook},
	}

	var rows []string
	rows = append(rows, "")

	for _, item := range items {
		count := m.viewTaskCount(item.view)
		selected := m.activeView == item.view

		label := fmt.Sprintf(" %s %s", item.icon, item.label)
		if count > 0 {
			label = fmt.Sprintf(" %s %-8s %d", item.icon, item.label, count)
		}

		style := lipgloss.NewStyle().Width(width - 2)

		if selected && isActive {
			style = style.
				Foreground(accent).
				Bold(true).
				Background(lipgloss.Color("#2a2a3a")).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(accent)
		} else if selected {
			style = style.
				Foreground(accent).
				Bold(true)
		} else {
			style = style.
				Foreground(lipgloss.Color("#999999"))
		}

		rows = append(rows, style.Render(label))
	}

	content := strings.Join(rows, "\n")

	paneStyle := lipgloss.NewStyle().
		Border(subtleBorder).
		BorderForeground(borderColor).
		Width(width).
		Height(height)

	return paneStyle.Render(content)
}

func (m Model) renderContent(width, height int) string {
	isActive := m.focus == focusContent
	accent := lipgloss.Color(m.cfg.Theme.Accent)
	borderColor := lipgloss.Color("#3a3a3a")
	if isActive {
		borderColor = accent
	}

	var body string
	switch m.activeView {
	case viewToday:
		body = m.renderTodayView(width-4, height-3)
	case viewUpcoming:
		body = m.renderUpcomingView(width-4, height-3)
	case viewLogbook:
		body = m.renderLogbookView(width-4, height-3)
	}

	paneStyle := lipgloss.NewStyle().
		Border(subtleBorder).
		BorderForeground(borderColor).
		Width(width).
		Height(height)

	return paneStyle.Render(body)
}

func (m Model) renderTodayView(maxWidth, maxHeight int) string {
	today := time.Now()
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
		Bold(true)
	title := titleStyle.Render(fmt.Sprintf("  Today · %s", today.Format("Jan 02")))

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")

	isActive := m.focus == focusContent

	if len(m.todayTasks) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Muted)).
			Italic(true).
			PaddingLeft(2)
		rows = append(rows, emptyStyle.Render("No tasks for today"))
		return strings.Join(rows, "\n")
	}

	flatIdx := 0
	for i, taskIdx := range m.todayTasks {
		if i == m.overdueStart && m.overdueStart > 0 && m.overdueStart < len(m.todayTasks) {
			sepStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.cfg.Theme.Overdue))
			sep := fmt.Sprintf("  ── Overdue %s", strings.Repeat("─", max(0, maxWidth-14)))
			rows = append(rows, sepStyle.Render(sep))
		}
		selected := flatIdx == m.contentCursor && isActive
		isOverdue := i >= m.overdueStart && m.overdueStart < len(m.todayTasks) && m.overdueStart > 0
		row := m.renderTaskRow(m.allTasks[taskIdx], selected, maxWidth, isOverdue)
		rows = append(rows, row)
		flatIdx++
	}

	return strings.Join(rows, "\n")
}

func (m Model) renderUpcomingView(maxWidth, maxHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
		Bold(true)
	title := titleStyle.Render("  Upcoming")

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")

	isActive := m.focus == focusContent

	if len(m.upcomingGroups) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Muted)).
			Italic(true).
			PaddingLeft(2)
		rows = append(rows, emptyStyle.Render("Nothing upcoming"))
		return strings.Join(rows, "\n")
	}

	flatIdx := 0
	for _, g := range m.upcomingGroups {
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Upcoming))
		header := fmt.Sprintf("  ── %s %s", g.Label, strings.Repeat("─", max(0, maxWidth-len(g.Label)-6)))
		rows = append(rows, headerStyle.Render(header))

		for _, taskIdx := range g.Tasks {
			selected := flatIdx == m.contentCursor && isActive
			row := m.renderTaskRow(m.allTasks[taskIdx], selected, maxWidth, false)
			rows = append(rows, row)
			flatIdx++
		}
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (m Model) renderLogbookView(maxWidth, maxHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
		Bold(true)
	title := titleStyle.Render("  Logbook")

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")

	if len(m.logbookGroups) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Muted)).
			Italic(true).
			PaddingLeft(2)
		rows = append(rows, emptyStyle.Render("Logbook is empty"))
		return strings.Join(rows, "\n")
	}

	isActive := m.focus == focusContent
	flatIdx := 0
	for _, g := range m.logbookGroups {
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Muted))
		header := fmt.Sprintf("  ── %s %s", g.Label, strings.Repeat("─", max(0, maxWidth-len(g.Label)-6)))
		rows = append(rows, headerStyle.Render(header))

		for _, taskIdx := range g.Tasks {
			selected := flatIdx == m.contentCursor && isActive
			row := m.renderLogbookTaskRow(m.allTasks[taskIdx], selected, maxWidth)
			rows = append(rows, row)
			flatIdx++
		}
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (m Model) renderTaskRow(task Task, selected bool, maxWidth int, isOverdue bool) string {
	bullet := "○"
	bulletColor := lipgloss.Color("#888888")
	if task.Done {
		bullet = "●"
		bulletColor = lipgloss.Color(m.cfg.Theme.Done)
	}
	if isOverdue {
		bulletColor = lipgloss.Color(m.cfg.Theme.Overdue)
	}

	bulletStyle := lipgloss.NewStyle().
		Foreground(bulletColor).
		PaddingLeft(2)

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
	if isOverdue {
		descStyle = descStyle.Foreground(lipgloss.Color(m.cfg.Theme.Overdue))
	}

	var tagParts []string
	for _, tag := range task.Tags {
		tStyle := lipgloss.NewStyle().Foreground(tagColor(tag))
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

func (m Model) renderLogbookTaskRow(task Task, selected bool, maxWidth int) string {
	bulletStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.cfg.Theme.Muted)).
		PaddingLeft(2)

	desc := task.Description
	descMaxWidth := maxWidth - 8
	if descMaxWidth < 10 {
		descMaxWidth = 10
	}
	if len(desc) > descMaxWidth {
		desc = desc[:descMaxWidth-3] + "..."
	}

	descStyle := lipgloss.NewStyle().
		PaddingLeft(1).
		Foreground(lipgloss.Color(m.cfg.Theme.Muted)).
		Strikethrough(true)

	var tagParts []string
	for _, tag := range task.Tags {
		tStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.cfg.Theme.Muted))
		tagParts = append(tagParts, tStyle.Render(tag))
	}
	tagStr := strings.Join(tagParts, " ")

	line := bulletStyle.Render("●") + descStyle.Render(desc)
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
	var statusPart string
	if m.statusMsg != "" && time.Since(m.statusTime) < 3*time.Second {
		statusStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Bold(true)
		statusPart = statusStyle.Render(" "+m.statusMsg) + "  "
	}

	keys := "n new  d done  e edit  D del  / filter  ? help  q quit"

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	filterInfo := ""
	if m.filter != "" {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.cfg.Theme.Accent)).
			Italic(true)
		filterInfo = filterStyle.Render(" [filter: " + m.filter + "] ")
	}

	return statusPart + filterInfo + keyStyle.Render(keys)
}

func (m Model) renderHelp() string {
	helpText := `
  Obsidian Tasks TUI

  Navigation
    j/k  ↑/↓       Move up/down
    h/l             Sidebar / Content
    Tab             Toggle focus
    1/2/3           Today / Upcoming / Logbook
    Enter           Select / toggle done

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
