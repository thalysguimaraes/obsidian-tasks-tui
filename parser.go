package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Task represents a single parsed Obsidian task.
type Task struct {
	Description    string
	Done           bool
	Tags           []string
	DueDate        time.Time
	CompletionDate time.Time
	FilePath       string // absolute path to the daily note
	LineNumber     int    // 1-based line number in the file
	RawLine        string // original line text
}

var (
	taskRe       = regexp.MustCompile(`^(\s*)-\s\[([ xX])\]\s*(.*)$`)
	tagRe        = regexp.MustCompile(`#[\w]+(?:/[\w]+)*`)
	dueDateRe    = regexp.MustCompile(`📅\s*(\d{4}-\d{2}-\d{2})`)
	doneDateRe   = regexp.MustCompile(`✅\s*(\d{4}-\d{2}-\d{2})`)
)

// ParseTask parses a single markdown line into a Task, if it matches.
// noteDate is the date derived from the daily note filename (fallback due date).
func ParseTask(line string, filePath string, lineNumber int, noteDate time.Time) (*Task, bool) {
	m := taskRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}

	done := m[2] == "x" || m[2] == "X"
	rest := m[3]

	// Extract tags
	tags := tagRe.FindAllString(rest, -1)

	// Extract due date
	var dueDate time.Time
	if dm := dueDateRe.FindStringSubmatch(rest); dm != nil {
		if t, err := time.Parse("2006-01-02", dm[1]); err == nil {
			dueDate = t
		}
	}
	if dueDate.IsZero() {
		dueDate = noteDate
	}

	// Extract completion date
	var completionDate time.Time
	if cm := doneDateRe.FindStringSubmatch(rest); cm != nil {
		if t, err := time.Parse("2006-01-02", cm[1]); err == nil {
			completionDate = t
		}
	}

	// Build description: everything except tags and date markers
	desc := rest
	desc = tagRe.ReplaceAllString(desc, "")
	desc = dueDateRe.ReplaceAllString(desc, "")
	desc = doneDateRe.ReplaceAllString(desc, "")
	desc = strings.TrimSpace(desc)

	return &Task{
		Description:    desc,
		Done:           done,
		Tags:           tags,
		DueDate:        dueDate,
		CompletionDate: completionDate,
		FilePath:       filePath,
		LineNumber:     lineNumber,
		RawLine:        line,
	}, true
}

// ParseFile reads a daily note and extracts tasks within the given section.
// If sectionHeading is empty, all tasks in the file are returned.
func ParseFile(filePath string, noteDate time.Time, sectionHeading string) ([]Task, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tasks []Task
	scanner := bufio.NewScanner(f)
	lineNum := 0
	inSection := sectionHeading == ""
	sectionLevel := ""
	if sectionHeading != "" {
		for _, ch := range sectionHeading {
			if ch == '#' {
				sectionLevel += "#"
			} else {
				break
			}
		}
	}

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if sectionHeading != "" {
			if trimmed == sectionHeading {
				inSection = true
				continue
			}
			if inSection && strings.HasPrefix(trimmed, sectionLevel+" ") && !strings.HasPrefix(trimmed, sectionLevel+"#") {
				inSection = false
				continue
			}
		}

		if inSection {
			if t, ok := ParseTask(line, filePath, lineNum, noteDate); ok {
				tasks = append(tasks, *t)
			}
		}
	}
	return tasks, scanner.Err()
}

// ScanDailyNotes scans the daily notes directory for tasks within the configured date range.
func ScanDailyNotes(cfg Config) ([]Task, error) {
	dir := filepath.Join(cfg.Vault.Path, cfg.Vault.DailyNotesDir)
	today := time.Now().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -cfg.Tasks.LogbookDays)
	end := today.AddDate(0, 0, cfg.Tasks.LookaheadDays)

	var allTasks []Task

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		filename := d.Format(cfg.Vault.DailyNoteFormat) + ".md"
		fp := filepath.Join(dir, filename)
		if _, err := os.Stat(fp); os.IsNotExist(err) {
			continue
		}
		tasks, err := ParseFile(fp, d, cfg.Tasks.SectionHeading)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", fp, err)
		}
		for _, t := range tasks {
			if len(t.Tags) == 0 {
				continue
			}
			excluded := false
			for _, tag := range t.Tags {
				for _, ex := range cfg.Tasks.ExcludeTags {
					if tag == ex || strings.HasPrefix(tag, ex+"/") {
						excluded = true
						break
					}
				}
				if excluded {
					break
				}
			}
			if !excluded {
				allTasks = append(allTasks, t)
			}
		}
	}

	return allTasks, nil
}

// ToggleDone toggles a task's done status in the file.
func ToggleDone(task *Task) error {
	lines, err := readLines(task.FilePath)
	if err != nil {
		return err
	}
	idx := task.LineNumber - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line %d out of range", task.LineNumber)
	}

	line := lines[idx]
	if task.Done {
		// Undo: [x] → [ ], remove ✅ date
		line = strings.Replace(line, "[x]", "[ ]", 1)
		line = strings.Replace(line, "[X]", "[ ]", 1)
		line = doneDateRe.ReplaceAllString(line, "")
		line = strings.TrimRight(line, " ")
		task.Done = false
		task.CompletionDate = time.Time{}
	} else {
		// Done: [ ] → [x], append ✅ date
		line = strings.Replace(line, "[ ]", "[x]", 1)
		today := time.Now().Format("2006-01-02")
		line = line + " ✅ " + today
		task.Done = true
		task.CompletionDate = time.Now().Truncate(24 * time.Hour)
	}

	lines[idx] = line
	task.RawLine = line
	return writeLines(task.FilePath, lines)
}

// DeleteTask removes a task line from its file.
func DeleteTask(task *Task) error {
	lines, err := readLines(task.FilePath)
	if err != nil {
		return err
	}
	idx := task.LineNumber - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line %d out of range", task.LineNumber)
	}
	lines = append(lines[:idx], lines[idx+1:]...)
	return writeLines(task.FilePath, lines)
}

// CreateTask appends a new task to the appropriate daily note file.
func CreateTask(cfg Config, description string, dueDate time.Time) error {
	dir := filepath.Join(cfg.Vault.Path, cfg.Vault.DailyNotesDir)
	filename := dueDate.Format(cfg.Vault.DailyNoteFormat) + ".md"
	fp := filepath.Join(dir, filename)

	taskLine := fmt.Sprintf("- [ ] %s 📅 %s", description, dueDate.Format("2006-01-02"))

	// If file doesn't exist, create with template
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		content := fmt.Sprintf(`---
created: %s
---

%s

%s

---
`, dueDate.Format("2006-01-02"), cfg.Tasks.SectionHeading, taskLine)
		return os.WriteFile(fp, []byte(content), 0644)
	}

	// File exists — insert under section heading
	lines, err := readLines(fp)
	if err != nil {
		return err
	}

	insertIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == cfg.Tasks.SectionHeading {
			insertIdx = i + 1
			// Skip blank lines after heading
			for insertIdx < len(lines) && strings.TrimSpace(lines[insertIdx]) == "" {
				insertIdx++
			}
			break
		}
	}

	if insertIdx == -1 {
		// Heading not found, append at end
		lines = append(lines, "", cfg.Tasks.SectionHeading, "", taskLine)
	} else {
		// Insert at position
		lines = append(lines[:insertIdx], append([]string{taskLine}, lines[insertIdx:]...)...)
	}

	return writeLines(fp, lines)
}

// UpdateTaskLine replaces a task's line in its file with new text.
func UpdateTaskLine(task *Task, newLine string) error {
	lines, err := readLines(task.FilePath)
	if err != nil {
		return err
	}
	idx := task.LineNumber - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line %d out of range", task.LineNumber)
	}
	lines[idx] = newLine
	task.RawLine = newLine
	return writeLines(task.FilePath, lines)
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	if content == "" {
		return []string{}, nil
	}
	// Preserve trailing newline by not trimming
	lines := strings.Split(content, "\n")
	// Remove last empty element caused by trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

func writeLines(path string, lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}
