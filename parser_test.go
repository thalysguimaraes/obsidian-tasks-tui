package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildTaskLinePreservesMetadata(t *testing.T) {
	dueDate := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.Local)
	doneDate := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.Local)

	line := buildTaskLine("Ship release", []string{"#work", "#ship"}, PriorityHigh, dueDate, true, doneDate)

	expected := "- [x] Ship release #work #ship ⏫ 📅 2026-03-11 ✅ 2026-03-10"
	if line != expected {
		t.Fatalf("unexpected task line\nexpected: %s\nactual:   %s", expected, line)
	}
}

func TestCreateFollowUpTaskCreatesTomorrowNote(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Vault.Path = vaultDir
	cfg.Vault.DailyNotesDir = "Daily"
	cfg.Tasks.SectionHeading = "## Tasks"

	task := Task{
		Description: "Send proposal",
		Tags:        []string{"#work/client"},
		Priority:    PriorityMedium,
	}

	followUpDate, err := CreateFollowUpTask(cfg, task)
	if err != nil {
		t.Fatalf("CreateFollowUpTask returned error: %v", err)
	}

	expectedDate := localToday().AddDate(0, 0, 1)
	if !followUpDate.Equal(expectedDate) {
		t.Fatalf("unexpected follow-up date\nexpected: %s\nactual:   %s", expectedDate.Format("2006-01-02"), followUpDate.Format("2006-01-02"))
	}

	notePath := filepath.Join(vaultDir, "Daily", followUpDate.Format(cfg.Vault.DailyNoteFormat)+".md")
	content, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("failed to read follow-up note: %v", err)
	}

	body := string(content)
	if !strings.Contains(body, "## Tasks") {
		t.Fatalf("expected section heading in note, got:\n%s", body)
	}

	expectedTask := "- [ ] Follow up: Send proposal #work/client 🔼 📅 " + followUpDate.Format("2006-01-02")
	if !strings.Contains(body, expectedTask) {
		t.Fatalf("expected follow-up task in note, got:\n%s", body)
	}
}
