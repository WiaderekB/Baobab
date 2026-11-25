package main

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	setupSketch = "setup-sketch"
	setupBoard  = "setup-board"
	setupPort   = "setup-port"
)

type setupDraft struct {
	sketch  string
	profile profile
}

func (m *model) loadSetup(step string) tea.Cmd {
	switch step {
	case setupSketch:
		root := m.root
		return m.load(step, "Setup 1/3 · Sketch", func(ctx context.Context) ([]item, error) {
			return sketches(ctx, root)
		})
	case setupBoard:
		return m.load(step, "Setup 2/3 · Board", boards)
	case setupPort:
		return m.load(step, "Setup 3/3 · USB port", ports)
	}
	return nil
}

func (m *model) chooseSetup(row item) tea.Cmd {
	switch m.purpose {
	case setupSketch:
		if m.setup.sketch != row.value {
			m.setup = setupDraft{sketch: row.value, profile: m.settingsFor(row.value)}
		}
		return m.loadSetup(setupBoard)
	case setupBoard:
		m.setup.profile.Board = selectedBoard(m.setup.profile.Board, row.value)
		return m.loadSetup(setupPort)
	case setupPort:
		if row.value != "" {
			m.setup.profile.Port = row.value
		}
		if m.remember(m.setup.sketch, &m.setup.profile) {
			m.mode = menu
			m.notice = "Setup saved for " + filepath.Base(m.sketch) + "."
			m.menuCursor = 0
		}
	}
	return nil
}

func baseBoard(fqbn string) string {
	parts := strings.SplitN(fqbn, ":", 4)
	return strings.Join(parts[:min(len(parts), 3)], ":")
}

func selectedBoard(current, selected string) string {
	if baseBoard(current) == selected {
		return current
	}
	return selected
}

func (m *model) preselect() {
	var value string
	switch m.purpose {
	case "sketch":
		value = m.sketch
	case "boards":
		value = baseBoard(m.settings().Board)
	case "ports":
		value = m.settings().Port
	case setupSketch:
		value = m.setup.sketch
	case setupBoard:
		value = baseBoard(m.setup.profile.Board)
	case setupPort:
		value = m.setup.profile.Port
	}
	if value == "" {
		return
	}
	for i, row := range m.rows {
		if row.value == value {
			m.cursor = i
			return
		}
	}
}
