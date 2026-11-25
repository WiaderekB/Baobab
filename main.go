package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type mode int

const (
	menu mode = iota
	picker
	input
	confirm
	busy
	report
)

type model struct {
	root, sketch                              string
	preferences                               preferences
	preferencesFile                           string
	setup                                     setupDraft
	preferencesError                          error
	menuName, purpose, title, query, notice   string
	mode                                      mode
	rows                                      []item
	cursor, menuCursor, scroll, width, height int
	selected                                  item
	jobID                                     int
	start, stageStart                         time.Time
	cancel                                    context.CancelCauseFunc
	progress                                  *progress
	stageDone                                 chan struct{}
	retryDiscovery, cancelRequested           bool
	stages                                    []stage
	stageIndex                                int
	releaseBuild                              func()
	log, buildStats                           string
	buildReport, fullLog                      bool
}

type loaded struct {
	id    int
	items []item
	err   error
}
type stageDone struct {
	id     int
	output string
	err    error
}
type tick struct{ id int }
type monitorDone struct{ err error }
type diagnosticsDone struct{ text string }

const author = "Bartosz Wiaderek"
const version = "0.3.2"

func (m *model) Init() tea.Cmd { return nil }

func (m *model) settings() profile {
	return m.settingsFor(m.sketch)
}

func (m *model) settingsFor(sketch string) profile {
	p := m.preferences.Profiles[sketch]
	if p.Board == "" {
		p.Board = "arduino:avr:uno"
	}
	if p.Baud == "" {
		p.Baud = "9600"
	}
	return p
}

func (m *model) remember(sketch string, update *profile) bool {
	if m.preferencesError != nil {
		m.notice = "Cannot save settings: " + m.preferencesError.Error()
		return false
	}
	settings, err := saveSelection(m.preferencesFile, m.root, sketch, update)
	if err != nil {
		m.notice = "Could not save settings: " + err.Error()
		return false
	}
	m.preferences, m.sketch = settings, sketch
	return true
}

func (m *model) save(p profile) {
	if m.sketch == "" {
		m.notice = "Choose a sketch first."
		return
	}
	if m.remember(m.sketch, &p) {
		m.notice = "Saved settings for " + filepath.Base(m.sketch) + "."
	}
}

func entry(label, detail, value string) item {
	return item{label: label, detail: detail, value: value}
}

func menuEntry(shortcut, label, detail, value string) item {
	return item{label: label, detail: detail, value: value, shortcut: shortcut}
}

func (m *model) menuItems() []item {
	switch m.menuName {
	case "Tools":
		return []item{
			menuEntry("U", "Upload Speed", uploadSpeedLabel(m.settings().UploadSpeed), "upload-speed"),
			menuEntry("C", "Clean build", "Rebuild everything when troubleshooting", "clean"),
			menuEntry("D", "Diagnostics", "Check Arduino CLI, the board core and USB ports", "doctor"),
			menuEntry("A", "About Baobab", "Version and author", "about"),
		}
	case "Board":
		return []item{
			menuEntry("M", "Board model", "Choose from installed models", "boards"),
			menuEntry("U", "USB port", "Find your board's serial port", "ports"),
			menuEntry("S", "Serial baud rate", "Set the serial monitor speed", "baud"),
			menuEntry("O", "Board options", "Set board-specific options", "options"),
		}
	case "Libraries":
		return []item{
			menuEntry("F", "Find and install", "Search the Arduino library index", "lib-search"),
			menuEntry("I", "Installed", "View names and versions", "lib-list"),
			menuEntry("R", "Refresh index", "Download the library index", "lib-index"),
		}
	case "Packages":
		return []item{
			menuEntry("F", "Find and install", "Browse available board packages", "core-search"),
			menuEntry("I", "Installed", "View installed versions", "core-list"),
			menuEntry("P", "Popular boards", "Uno, ESP32, ESP8266, Nano 33 BLE", "presets"),
			menuEntry("R", "Refresh index", "Download the board package index", "core-index"),
			menuEntry("A", "Add index URL", "Add support for another vendor", "core-url"),
		}
	default:
		return []item{
			menuEntry("U", "Upload", "Build and flash the board", "upload"),
			menuEntry("C", "Build", "Compile the sketch", "compile"),
			menuEntry("M", "Monitor", "Read and send serial data", "monitor"),
			menuEntry("S", "Setup", "Choose a sketch, board and USB port", setupSketch),
			menuEntry("F", "Sketch", "Choose a sketch", "sketch"),
			menuEntry("B", "Board", "Change the model, port or baud rate", "Board"),
			menuEntry("L", "Libraries", "Find and install libraries", "Libraries"),
			menuEntry("P", "Packages", "Install board support", "Packages"),
			menuEntry("T", "Tools", "Clean build, diagnostics and about", "Tools"),
		}
	}
}

func (m *model) back() {
	m.query, m.notice = "", ""
	if m.mode != menu {
		m.mode = menu
		return
	}
	m.menuName = ""
	m.menuCursor = 0
}

func (m *model) openPicker(purpose, title string, rows []item) {
	m.mode, m.purpose, m.title, m.rows = picker, purpose, title, rows
	m.cursor, m.query, m.notice = 0, "", ""
}

func (m *model) ask(purpose, title, initial string) {
	m.mode, m.purpose, m.title, m.query = input, purpose, title, initial
}

func (m *model) filtered() []item {
	q := strings.Fields(strings.ToLower(m.query))
	var result []item
	for _, row := range m.rows {
		text := strings.ToLower(row.label + " " + row.detail)
		ok := true
		for _, word := range q {
			if !strings.Contains(text, word) {
				ok = false
				break
			}
		}
		if ok {
			result = append(result, row)
		}
	}
	return result
}

func pulse(id int) tea.Cmd {
	return tea.Tick(125*time.Millisecond, func(time.Time) tea.Msg { return tick{id} })
}

func (m *model) load(purpose, title string, fn func(context.Context) ([]item, error)) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	m.cancel = func(error) { cancel() }
	m.progress, m.cancelRequested = nil, false
	m.jobID++
	id := m.jobID
	m.mode, m.purpose, m.title, m.start = busy, purpose, title, time.Now()
	m.stages = nil
	return tea.Batch(pulse(id), func() tea.Msg {
		defer cancel()
		rows, err := fn(ctx)
		return loaded{id, rows, err}
	})
}

func (m *model) run(stages []stage) tea.Cmd {
	m.stages, m.stageIndex, m.log, m.buildStats = stages, 0, "", ""
	m.mode, m.start, m.cancelRequested = busy, time.Now(), false
	m.jobID++
	return tea.Batch(pulse(m.jobID), m.nextStage())
}

func (m *model) nextStage() tea.Cmd {
	ctx, cancel := context.WithCancelCause(context.Background())
	m.cancel = cancel
	output, done := &progress{}, make(chan struct{})
	m.progress, m.stageDone, m.stageStart = output, done, time.Now()
	id, s := m.jobID, m.stages[m.stageIndex]
	m.title = s.title
	return func() tea.Msg {
		defer close(done)
		defer cancel(nil)
		out, err := runStage(ctx, s, output)
		return stageDone{id, out, err}
	}
}

func (m *model) finish(title, output string) {
	m.mode, m.title, m.log, m.scroll = report, title, output, 0
	m.buildReport, m.fullLog = m.releaseBuild != nil, false
	m.cancel, m.progress, m.stageDone = nil, nil, nil
	m.retryDiscovery, m.cancelRequested = false, false
	if m.releaseBuild != nil {
		m.releaseBuild()
		m.releaseBuild = nil
	}
}

func (m *model) operation(action string) tea.Cmd {
	p := m.settings()
	if m.sketch == "" || !isSketch(m.sketch) {
		m.notice = "Choose a sketch in Setup or Sketch."
		return nil
	}
	if action == "upload" || action == "monitor" {
		return m.load(action+"-port", "Checking USB ports", ports)
	}
	return m.startOperation(action, p)
}

func (m *model) startOperation(action string, p profile) tea.Cmd {
	if action == "monitor" {
		m.notice = ""
		return tea.ExecProcess(useEnglish(exec.Command("arduino-cli", "monitor", "--port", p.Port,
			"--fqbn", p.Board, "--config", "baudrate="+p.Baud)), func(err error) tea.Msg { return monitorDone{err} })
	}
	dir, release, err := openBuildCache(m.sketch, p.Board)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	m.releaseBuild = release
	_, cacheErr := os.Stat(filepath.Join(dir, "includes.cache"))
	m.retryDiscovery = action != "clean" && cacheErr == nil
	return m.run(buildStages(m.sketch, p, dir, action == "upload", action == "clean"))
}

func (m *model) activate(action string) tea.Cmd {
	m.notice = ""
	switch action {
	case "upload", "compile", "clean", "monitor":
		return m.operation(action)
	case "about":
		m.finish("About Baobab", "Baobab "+version+"\n\nCreated by  "+author+"\n\nBuild and upload Arduino sketches, read serial data and manage libraries in the Zed terminal.\n\nGo · Bubble Tea · Arduino CLI")
	case "doctor":
		root, sketch, p := m.root, m.sketch, m.settings()
		m.jobID++
		m.mode, m.title, m.start, m.progress, m.stages = busy, "Checking your environment", time.Now(), nil, nil
		m.cancelRequested = false
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		m.cancel = func(error) { cancel() }
		return tea.Batch(pulse(m.jobID), func() tea.Msg {
			defer cancel()
			return diagnosticsDone{diagnostics(ctx, root, sketch, p)}
		})
	case "Board", "Libraries", "Packages", "Tools":
		m.menuName, m.menuCursor = action, 0
	case "upload-speed":
		m.openUploadSpeed()
	case setupSketch:
		m.setup = setupDraft{sketch: m.sketch, profile: m.settings()}
		return m.loadSetup(setupSketch)
	case "sketch":
		root := m.root
		return m.load(action, "Choose a sketch", func(ctx context.Context) ([]item, error) { return sketches(ctx, root) })
	case "boards", "ports", "baud", "options":
		if m.sketch == "" {
			m.notice = "Choose a sketch first."
			return nil
		}
		switch action {
		case "boards":
			return m.load(action, "Choose a board model", boards)
		case "ports":
			return m.load(action, "Choose a USB port", ports)
		case "baud":
			m.ask(action, "Serial baud rate, e.g. 9600 or 115200", m.settings().Baud)
		case "options":
			parts := strings.SplitN(m.settings().Board, ":", 4)
			current := ""
			if len(parts) == 4 {
				current = parts[3]
			}
			m.ask(action, "Board options, e.g. cpu=atmega328old (empty for defaults)", current)
		}
	case "lib-search":
		m.ask(action, "Enter a library name", "")
	case "lib-list":
		return m.load(action, "Installed biblioteki", func(ctx context.Context) ([]item, error) { return libraries(ctx, "", true) })
	case "core-search", "core-list":
		return m.load(action, "Board packages", func(ctx context.Context) ([]item, error) { return packages(ctx, action == "core-list") })
	case "lib-index":
		return m.run([]stage{{"Refreshing the library index", []string{"lib", "update-index"}, 3 * time.Minute}})
	case "core-index":
		return m.run([]stage{{"Refreshing the package index", []string{"core", "update-index"}, 3 * time.Minute}})
	case "core-url":
		m.ask(action, "Vendor index HTTPS URL", "")
	case "presets":
		m.openPicker(action, "Popular board packages", []item{
			entry("Arduino Uno / Nano / Mega", "arduino:avr", "arduino:avr"),
			entry("ESP32", "esp32:esp32", "esp32:esp32"),
			entry("ESP8266", "esp8266:esp8266", "esp8266:esp8266"),
			entry("Nano 33 BLE / Sense", "arduino:mbed_nano", "arduino:mbed_nano"),
		})
	}
	return nil
}

func (m *model) acceptText() tea.Cmd {
	value := strings.TrimSpace(m.query)
	p := m.settings()
	switch m.purpose {
	case "custom-upload-speed":
		speed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || speed <= 0 {
			m.notice = "Enter a positive baud rate."
			return nil
		}
		m.mode, p.UploadSpeed = menu, int(speed)
		m.save(p)
	case "baud":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			m.notice = "Enter a positive number."
			return nil
		}
		m.mode = menu
		p.Baud = strconv.Itoa(n)
		m.save(p)
	case "options":
		base := baseBoard(p.Board)
		if value != "" {
			for _, pair := range strings.Split(value, ",") {
				k, v, ok := strings.Cut(pair, "=")
				if !ok || k == "" || v == "" || strings.ContainsAny(pair, " :\t") {
					m.notice = "Use key=value,key=value."
					return nil
				}
			}
			base += ":" + value
		}
		m.mode, p.Board = menu, base
		m.save(p)
	case "lib-search":
		if value == "" {
			m.notice = "Enter all or part of a library name."
			return nil
		}
		return m.load("lib-install", "Search results", func(ctx context.Context) ([]item, error) { return libraries(ctx, value, false) })
	case "core-url":
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			m.notice = "Enter a complete HTTPS URL."
			return nil
		}
		m.selected = entry(value, "", value)
		m.mode = confirm
	}
	return nil
}

func (m *model) choose(row item) tea.Cmd {
	p := m.settings()
	switch m.purpose {
	case "upload-speed":
		if row.value == "custom" {
			current := ""
			if p.UploadSpeed > 0 {
				current = strconv.Itoa(p.UploadSpeed)
			}
			m.ask("custom-upload-speed", "Enter the upload speed in baud", current)
		} else {
			m.mode = menu
			p.UploadSpeed, _ = strconv.Atoi(row.value)
			m.save(p)
		}
	case setupSketch, setupBoard, setupPort:
		return m.chooseSetup(row)
	case "sketch":
		m.mode = menu
		m.sketch = row.value
		if m.remember(row.value, nil) {
			m.notice = "Selected " + row.label + "."
		}
	case "boards":
		m.mode, p.Board = menu, selectedBoard(p.Board, row.value)
		m.save(p)
	case "ports", "upload-port", "monitor-port":
		m.mode, p.Port = menu, row.value
		m.save(p)
		if m.purpose != "ports" {
			return m.startOperation(strings.TrimSuffix(m.purpose, "-port"), p)
		}
	case "lib-install", "core-search", "presets":
		m.selected, m.mode = row, confirm
	}
	return nil
}

func (m *model) install() tea.Cmd {
	value := m.selected.value
	switch m.purpose {
	case "core-url":
		return m.run([]stage{
			{"Saving the index URL", []string{"config", "add", "board_manager.additional_urls", value}, 20 * time.Second},
			{"Refreshing the package index", []string{"core", "update-index"}, 3 * time.Minute},
		})
	case "lib-install":
		return m.run([]stage{{"Installing library", []string{"lib", "install", "--", value}, 5 * time.Minute}})
	default:
		args := []string{}
		if m.purpose == "presets" {
			switch value {
			case "esp32:esp32":
				args = []string{"--additional-urls", "https://espressif.github.io/arduino-esp32/package_esp32_index.json"}
			case "esp8266:esp8266":
				args = []string{"--additional-urls", "https://arduino.esp8266.com/stable/package_esp8266com_index.json"}
			}
		}
		var stages []stage
		if len(args) > 0 {
			stages = append(stages, stage{"Saving the vendor index", []string{"config", "add", "board_manager.additional_urls", args[1]}, 20 * time.Second})
		}
		stages = append(stages,
			stage{"Refreshing the package index", []string{"core", "update-index"}, 3 * time.Minute},
			stage{"Installing package", []string{"core", "install", "--", value}, 20 * time.Minute})
		return m.run(stages)
	}
}

func move(pos, delta, count int) int { return max(0, min(pos+delta, count-1)) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.mode == report {
			m.scroll = min(m.scroll, max(0, len(m.reportLines())-m.pageSize()))
		}
	case tick:
		if m.mode == busy && m.jobID == msg.id {
			if m.retryDiscovery && m.progress != nil && m.stageIndex == 0 {
				phase, _, last := m.progress.status()
				if phase == "Resolving libraries" && time.Since(last) >= discoveryTimeout {
					m.cancel(errStalledDiscovery)
				}
			}
			return m, pulse(msg.id)
		}
	case loaded:
		if m.jobID != msg.id {
			break
		}
		m.cancel = nil
		if m.cancelRequested || errors.Is(msg.err, context.Canceled) {
			m.finish("Canceled", "")
			m.notice = ""
			break
		}
		if msg.err != nil {
			m.finish("Error", msg.err.Error())
			break
		}
		if m.purpose == "upload-port" || m.purpose == "monitor-port" {
			p := m.settings()
			for _, row := range msg.items {
				if row.value == p.Port {
					m.mode = menu
					return m, m.startOperation(strings.TrimSuffix(m.purpose, "-port"), p)
				}
			}
			m.title = "Choose a USB port to continue"
		}
		if (m.purpose == "ports" || m.purpose == "upload-port" || m.purpose == "monitor-port") && len(msg.items) == 0 {
			m.finish("No USB port detected", "Connect the board using a USB data cable, then try again.")
			break
		}
		if (m.purpose == "sketch" || m.purpose == setupSketch) && len(msg.items) == 0 {
			m.finish("No sketches found", "Searched in: "+m.root+"\nExpected layout: Name/Name.ino")
			break
		}
		if m.purpose == setupBoard && len(msg.items) == 0 {
			m.finish("No boards installed", "Install support for your board from Packages, then run Setup again.")
			break
		}
		rows := msg.items
		if m.purpose == setupPort {
			label := "Set up USB later"
			if m.setup.profile.Port != "" {
				label = "Keep saved port"
			}
			rows = append(rows, entry(label, "Continue setup without a connected board", ""))
		}
		m.openPicker(m.purpose, m.title, rows)
		m.preselect()
	case stageDone:
		if m.jobID != msg.id {
			break
		}
		if msg.output != "" {
			m.log += "── " + m.title + " · " + time.Since(m.stageStart).Round(100*time.Millisecond).String() + " ──\n" + msg.output + "\n\n"
		}
		if errors.Is(msg.err, errStalledDiscovery) && m.retryDiscovery {
			m.retryDiscovery = false
			m.notice = "Library discovery stalled. Retrying with a clean build."
			s := &m.stages[m.stageIndex]
			s.args = append([]string{"compile", "--clean"}, s.args[1:]...)
			s.title = "Clean build"
			return m, m.nextStage()
		}
		m.notice = ""
		if m.cancelRequested || errors.Is(msg.err, context.Canceled) {
			m.finish("Canceled", m.log)
			break
		}
		if msg.err != nil {
			m.finish("Operation failed", msg.err.Error()+"\n\n"+m.log)
			m.scroll = max(0, len(m.reportLines())-m.pageSize())
			break
		}
		if m.stages[m.stageIndex].args[0] == "compile" {
			m.buildStats = m.progress.librarySummary()
		}
		m.stageIndex++
		if m.stageIndex < len(m.stages) {
			return m, m.nextStage()
		}
		m.finish("Done · "+time.Since(m.start).Round(100*time.Millisecond).String(), m.buildStats+buildSummary(m.log)+m.log)
	case diagnosticsDone:
		m.notice = ""
		if m.cancelRequested {
			m.finish("Canceled", "")
		} else {
			m.finish("Diagnostics", msg.text)
		}
	case monitorDone:
		if msg.err != nil {
			m.notice = "Serial monitor closed: " + msg.err.Error()
		} else {
			m.notice = "Serial monitor closed."
		}
	case tea.PasteMsg:
		if m.mode == picker || m.mode == input {
			m.query += strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return -1
				}
				return r
			}, msg.Content)
			m.cursor = 0
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if m.mode == busy {
			if (key == "esc" || key == "ctrl+c") && m.cancel != nil {
				m.cancelRequested = true
				m.cancel(context.Canceled)
				m.retryDiscovery = false
				m.notice = "Stopping…"
			}
			break
		}
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if key == "esc" {
			if m.mode == input && m.purpose == "custom-upload-speed" {
				m.openUploadSpeed()
				break
			}
			if m.mode == picker {
				switch m.purpose {
				case setupBoard:
					return m, m.loadSetup(setupSketch)
				case setupPort:
					return m, m.loadSetup(setupBoard)
				}
			}
			if m.mode == confirm && m.purpose != "core-url" {
				m.mode = picker
			} else {
				m.back()
			}
			break
		}
		if (key == "q" || key == "Q") && m.mode == menu && m.menuName == "" {
			return m, tea.Quit
		}
		switch m.mode {
		case menu:
			rows := m.menuItems()
			switch key {
			case "up", "k", "shift+tab":
				m.menuCursor = move(m.menuCursor, -1, len(rows))
			case "down", "j", "tab":
				m.menuCursor = move(m.menuCursor, 1, len(rows))
			case "home":
				m.menuCursor = 0
			case "end":
				m.menuCursor = len(rows) - 1
			case "enter":
				return m, m.activate(rows[m.menuCursor].value)
			default:
				for i, row := range rows {
					if strings.EqualFold(key, row.shortcut) {
						m.menuCursor = i
						return m, m.activate(row.value)
					}
				}
			}
		case picker, input:
			switch key {
			case "ctrl+r":
				if m.mode == picker && m.purpose == setupPort {
					return m, m.loadSetup(setupPort)
				}
			case "up", "down", "shift+tab", "tab", "pgup", "pgdown", "home", "end":
				if m.mode == picker {
					count := len(m.filtered())
					delta := map[string]int{
						"up": -1, "shift+tab": -1, "down": 1, "tab": 1,
						"pgup": -m.pageSize(), "pgdown": m.pageSize(), "home": -count, "end": count,
					}
					m.cursor = move(m.cursor, delta[key], count)
				}
			case "enter":
				if m.mode == input {
					return m, m.acceptText()
				}
				rows := m.filtered()
				if m.cursor < len(rows) {
					return m, m.choose(rows[m.cursor])
				}
			case "ctrl+u":
				m.query, m.cursor = "", 0
			case "backspace":
				runes := []rune(m.query)
				if len(runes) > 0 {
					m.query = string(runes[:len(runes)-1])
					m.cursor = 0
				}
			default:
				if s := msg.Key().Text; s != "" {
					m.query += s
					m.cursor = 0
				}
			}
		case confirm:
			if key == "enter" {
				return m, m.install()
			}
		case report:
			switch key {
			case "enter":
				m.back()
			case "l", "L":
				if m.buildReport {
					m.fullLog = !m.fullLog
					m.scroll = 0
				}
			case "home":
				m.scroll = 0
			case "end":
				m.scroll = max(0, len(m.reportLines())-m.pageSize())
			case "up", "down", "pgup", "pgdown":
				delta := map[string]int{"up": -1, "down": 1, "pgup": -m.pageSize(), "pgdown": m.pageSize()}
				m.scroll = max(0, min(m.scroll+delta[key], len(m.reportLines())-m.pageSize()))
			}
		}
	}
	return m, nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Baobab — Arduino in the terminal\nUsage: bao [-root workspace] [workspace | sketch folder | .ino file]")
	}
	workspace := flag.String("root", "", "workspace to search for sketches")
	showVersion := flag.Bool("version", false, "print the application version")
	flag.Parse()
	if *showVersion {
		fmt.Println("Baobab " + version)
		return
	}
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := "."
	if flag.NArg() == 1 {
		path = flag.Arg(0)
	}
	root, sketch, err := startPath(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Baobab:", err)
		os.Exit(1)
	}
	if *workspace != "" {
		dir, err := filepath.Abs(*workspace)
		if err == nil {
			dir, err = filepath.EvalSymlinks(dir)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Baobab:", err)
			os.Exit(1)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			fmt.Fprintln(os.Stderr, "Baobab: invalid workspace folder:", dir)
			os.Exit(1)
		}
		root = dir
	}
	if _, err := exec.LookPath("arduino-cli"); err != nil {
		fmt.Fprintln(os.Stderr, "Baobab: arduino-cli was not found on PATH.")
		os.Exit(1)
	}
	pathToSettings, preferencesError := preferencesPath()
	var settings preferences
	if preferencesError == nil {
		settings, preferencesError = readPreferences(pathToSettings)
	}
	if sketch == "" {
		if flag.NArg() == 0 && *workspace == "" {
			home, _ := os.UserHomeDir()
			if root == home && settings.LastWorkspace != "" && settings.rememberedSketch(settings.LastWorkspace) != "" {
				root = settings.LastWorkspace
			}
		}
		sketch = settings.rememberedSketch(root)
	}
	m := &model{
		root: root, sketch: sketch, preferences: settings, preferencesFile: pathToSettings,
		preferencesError: preferencesError, width: 80, height: 24,
	}
	if preferencesError != nil {
		m.notice = "Could not load settings: " + preferencesError.Error()
	} else if sketch != "" && (settings.LastSketch[root] != sketch || settings.LastWorkspace != root) {
		m.remember(sketch, nil)
	}
	_, err = tea.NewProgram(m).Run()
	if m.cancel != nil {
		m.cancel(context.Canceled)
	}
	if m.stageDone != nil {
		select {
		case <-m.stageDone:
		case <-time.After(3 * time.Second):
		}
	}
	if m.releaseBuild != nil {
		m.releaseBuild()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Baobab:", err)
		os.Exit(1)
	}
}
