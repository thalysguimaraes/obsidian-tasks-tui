package main

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Vault VaultConfig `toml:"vault"`
	Tasks TasksConfig `toml:"tasks"`
	Theme ThemeConfig `toml:"theme"`
}

type VaultConfig struct {
	Path            string `toml:"path"`
	DailyNotesDir   string `toml:"daily_notes_dir"`
	DailyNoteFormat string `toml:"daily_note_format"`
}

type TasksConfig struct {
	SectionHeading string `toml:"section_heading"`
	LookbackDays   int    `toml:"lookback_days"`
	LookaheadDays  int    `toml:"lookahead_days"`
}

type ThemeConfig struct {
	Accent   string `toml:"accent"`
	Overdue  string `toml:"overdue"`
	Today    string `toml:"today"`
	Upcoming string `toml:"upcoming"`
	Done     string `toml:"done"`
}

func DefaultConfig() Config {
	return Config{
		Vault: VaultConfig{
			Path:            "",
			DailyNotesDir:   "Notes/Daily Notes",
			DailyNoteFormat: "2006-01-02",
		},
		Tasks: TasksConfig{
			SectionHeading: "## :LiPencil: Open Space",
			LookbackDays:   7,
			LookaheadDays:  14,
		},
		Theme: ThemeConfig{
			Accent:   "#7571F9",
			Overdue:  "#FE5F86",
			Today:    "#1e90ff",
			Upcoming: "#888888",
			Done:     "#02BF87",
		},
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg, nil
		}
		path = filepath.Join(home, ".config", "obsidian-tasks", "config.toml")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
