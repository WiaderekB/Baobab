package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	accent = "1;38;5;78"
	badge  = "1;38;5;194;48;5;22"
	focus  = "1;38;5;194;48;5;29"
	warn   = "38;5;215"
	muted  = "2"
	strong = "1"
)

func paint(code, value string) string {
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (m *model) contentWidth() int {
	return min(94, max(1, m.width-4))
}

func (m *model) heading() string {
	name := paint(badge, " BAOBAB ")
	if m.menuName != "" {
		name += "   " + paint(strong, m.menuName)
	}
	if m.mode == menu && m.menuName == "" {
		gap := m.contentWidth() - ansi.StringWidth(name) - ansi.StringWidth(author)
		if gap >= 5 {
			name += strings.Repeat(" ", gap) + paint(muted, author)
		}
	}
	return name
}

func boardLabel(fqbn string) string {
	parts := strings.SplitN(fqbn, ":", 4)
	if len(parts) < 3 {
		return fqbn
	}
	switch strings.Join(parts[:3], ":") {
	case "arduino:avr:uno":
		return "Arduino Uno"
	case "arduino:mbed_nano:nano33ble":
		return "Nano 33 BLE"
	case "esp8266:esp8266:d1_mini":
		return "D1 mini (ESP8266)"
	case "esp8266:esp8266:nodemcuv2":
		return "NodeMCU (ESP8266)"
	case "esp32:esp32:esp32":
		return "ESP32 Dev Module"
	default:
		return strings.ReplaceAll(parts[2], "_", " ")
	}
}

func (m *model) contextLines() []string {
	p := m.settings()
	sketch := "choose a sketch"
	if m.sketch != "" {
		sketch = filepath.Base(m.sketch)
	}
	port := "not selected"
	if p.Port != "" {
		port = filepath.Base(p.Port)
	}
	w := m.contentWidth()
	field := func(label, value string) string {
		return paint(muted, label) + " " + paint(strong, value)
	}
	board := boardLabel(p.Board)
	usb := field("USB", port)
	const gap = "    "

	detail := field("SKETCH", sketch) + gap + field("BOARD", board)
	if ansi.StringWidth(detail+gap+usb) <= w {
		return []string{detail + gap + usb}
	}
	if ansi.StringWidth(detail) > w {
		boardWidth := max(1, min(ansi.StringWidth(board), w/2))
		sketchWidth := max(1, w-boardWidth-3)
		detail = paint(strong, ansi.Truncate(sketch, sketchWidth, "…")) +
			paint(muted, " · ") + ansi.Truncate(board, boardWidth, "…")
	}
	if m.height >= 15 {
		return []string{detail, usb}
	}
	return []string{detail}
}

func (m *model) pageSize() int {
	chrome := 4 + len(m.contextLines())
	if m.mode == picker {
		chrome += 3
	}
	if m.mode == report {
		chrome++
	}
	if m.notice != "" {
		chrome++
	}
	return max(1, m.height-chrome)
}

func (m *model) reportLines() []string {
	log := strings.ReplaceAll(ansi.Strip(m.log), "\r\n", "\n")
	log = strings.ReplaceAll(log, "\r", "\n")
	if m.buildReport && !m.fullLog {
		log = compactBuildLog(log)
	}
	return strings.Split(ansi.Wrap(log, m.contentWidth(), "/"), "\n")
}

func (m *model) renderChoices(line func(string), rows []item, selected int) {
	if len(rows) == 0 {
		line(paint(muted, "No results. Change the filter or press Esc to go back."))
		return
	}
	limit := m.pageSize()
	start := max(0, min(selected-limit/2, len(rows)-limit))
	w := m.contentWidth()
	labelWidth := 0
	shortcutWidth := 0
	if m.mode == menu {
		shortcutWidth = 4
	}
	if w >= 54 {
		for _, row := range rows {
			labelWidth = max(labelWidth, min(w/2-5-shortcutWidth, ansi.StringWidth(row.label)))
		}
	}
	for i := start; i < min(len(rows), start+limit); i++ {
		row := rows[i]
		prefix, color := "  ", strong
		if i == selected {
			prefix, color = "❯ ", focus
		}
		shortcut := ""
		if m.mode == menu {
			shortcut = paint(accent, "["+row.shortcut+"]") + " "
		}
		label := row.label
		if labelWidth > 0 {
			label = ansi.Truncate(label, labelWidth, "…")
			padding := strings.Repeat(" ", labelWidth-ansi.StringWidth(label)+2)
			line(shortcut + paint(color, prefix+label+" ") + padding + paint(muted, row.detail))
		} else {
			line(shortcut + paint(color, prefix+label+" "))
		}
	}
	if m.mode == picker {
		row := rows[selected]
		detail := row.detail
		if w >= 54 && detail == "" {
			detail = row.label
		}
		count := fmt.Sprintf("%d/%d", selected+1, len(rows))
		line(paint(warn, count) + "  " + paint(muted, detail))
	}
}

func (m *model) View() tea.View {
	var b strings.Builder
	w := m.contentWidth()
	line := func(s string) {
		fmt.Fprintln(&b, strings.Repeat(" ", min(2, max(0, m.width-1)))+ansi.Truncate(s, w, "…"))
	}
	title := func(s string) { line(paint(accent, s)) }
	rule := func() { line(paint(muted, strings.Repeat("─", w))) }

	line(m.heading())
	for _, s := range m.contextLines() {
		line(s)
	}
	rule()

	var help string
	switch m.mode {
	case menu:
		m.renderChoices(line, m.menuItems(), m.menuCursor)
		leave := "Esc back"
		if m.menuName == "" {
			leave = "Q quit"
		}
		help = "Letter open   Tab move   " + leave
		if w < ansi.StringWidth(help) {
			help = "Letter · Tab · " + leave
		}
	case picker:
		title(m.title)
		line(paint(muted, "Filter  ") + m.query + "▏")
		m.renderChoices(line, m.filtered(), m.cursor)
		help = "Tab / ↑↓ move   Enter select   Esc back"
		if w < 38 {
			help = "Tab · Enter · Esc"
		}
		if m.purpose == setupPort {
			help = "Tab move   Enter save   Ctrl+R refresh   Esc back"
			if w < 55 {
				help = "Tab · Enter · Ctrl+R · Esc"
			}
		}
	case input:
		title(m.title)
		queryWidth := max(1, w-4)
		query := m.query
		if ansi.StringWidth(query) > queryWidth {
			query = ansi.TruncateLeft(query, ansi.StringWidth(query)-queryWidth+1, "…")
		}
		line(paint(accent, "❯ ") + query + "▏")
		help = "Enter save   Esc back"
		if w >= 60 {
			help += "   Ctrl+U clear"
		}
	case confirm:
		line(paint(warn, "Confirm changes"))
		for _, s := range strings.Split(ansi.Wrap(m.selected.label+"\n"+m.selected.detail, w, "/"), "\n") {
			line(s)
		}
		help = "Enter confirm   Esc cancel"
	case busy:
		elapsed := time.Since(m.start)
		width := min(32, max(7, w-2))
		pos := int(elapsed/(125*time.Millisecond)) % (width - 4)
		title(m.title + "  " + elapsed.Round(100*time.Millisecond).String())
		if len(m.stages) > 1 {
			line(paint(muted, fmt.Sprintf("Step %d of %d", m.stageIndex+1, len(m.stages))))
		}
		if m.progress != nil {
			phase, detail, _ := m.progress.status()
			status := strings.Trim(strings.Join([]string{phase, detail}, " · "), " ·")
			if status != "" {
				line(paint(muted, status))
			}
		}
		line(paint(muted, "["+strings.Repeat(".", pos)) + paint(accent, "#####") + paint(muted, strings.Repeat(".", width-pos-5)+"]"))
		help = "Esc cancel"
	case report:
		color := accent
		if strings.HasPrefix(m.title, "Error") || strings.HasPrefix(m.title, "Operation failed") || strings.HasPrefix(m.title, "No ") || m.title == "Canceled" {
			color = warn
		}
		lines := m.reportLines()
		limit := m.pageSize()
		start := min(m.scroll, max(0, len(lines)-limit))
		end := min(len(lines), start+limit)
		label := paint(color, m.title)
		if len(lines) > limit {
			label += paint(muted, fmt.Sprintf("  ·  %d–%d / %d", start+1, end, len(lines)))
		}
		line(label)
		for _, s := range lines[start:end] {
			line(s)
		}
		help = "↑↓ scroll   Enter / Esc back"
		if m.buildReport {
			toggle := "L full log"
			if m.fullLog {
				toggle = "L brief log"
			}
			if w >= 51 {
				help += "   " + toggle
			} else {
				help = "↑↓ log · " + toggle + " · Esc"
			}
		}
	}
	rule()
	if m.notice != "" {
		line(paint(warn, m.notice))
	}
	line(paint(muted, help))
	v := tea.NewView(strings.TrimSuffix(b.String(), "\n"))
	v.AltScreen = true
	v.WindowTitle = "Baobab"
	return v
}
