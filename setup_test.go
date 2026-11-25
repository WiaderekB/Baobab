//go:build unix

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSetupRestoresAndSavesLegacyProfile(t *testing.T) {
	root := t.TempDir()
	sketch := filepath.Join(root, "Sensor")
	if err := os.Mkdir(sketch, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sketch, "Sensor.ino"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	original := profile{Board: "arduino:avr:nano:cpu=atmega328old", Port: "/dev/cu.saved", Baud: "115200"}
	settingsFile := filepath.Join(root, "profiles.json")
	legacy, err := json.Marshal(map[string]profile{sketch: original})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := readPreferences(settingsFile)
	if err != nil || settings.rememberedSketch(root) != sketch {
		t.Fatalf("legacy sketch was not restored: %v", err)
	}
	fakeCLI := `#!/bin/sh
case "$*" in
*"board listall"*) echo '{"boards":[{"name":"Uno","fqbn":"arduino:avr:uno"},{"name":"Nano","fqbn":"arduino:avr:nano"}]}' ;;
*"board list"*) echo '{"detected_ports":[{"port":{"address":"/dev/cu.other","protocol":"serial"}},{"port":{"address":"/dev/cu.saved","protocol":"serial"}}]}' ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "arduino-cli"), []byte(fakeCLI), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := &model{root: root, sketch: sketch, preferences: settings, preferencesFile: settingsFile}
	deliver := func(command tea.Cmd, step string) {
		t.Helper()
		messages := command().(tea.BatchMsg)
		m.Update(messages[1]())
		if m.mode != picker || m.purpose != step {
			t.Fatalf("expected %s, got %s in mode %d: %s", step, m.purpose, m.mode, m.log)
		}
	}
	deliver(m.activate(setupSketch), setupSketch)
	deliver(m.choose(m.rows[m.cursor]), setupBoard)
	if m.rows[m.cursor].value != "arduino:avr:nano" {
		t.Fatal("saved board was not preselected")
	}
	deliver(m.choose(m.rows[m.cursor]), setupPort)
	if m.rows[m.cursor].value != original.Port {
		t.Fatal("saved port was not preselected")
	}
	m.choose(m.rows[0]) // Save a different port.
	reloaded, err := readPreferences(settingsFile)
	if err != nil {
		t.Fatal(err)
	}
	original.Port = "/dev/cu.other"
	if reloaded.LastSketch[root] != sketch || reloaded.Profiles[sketch] != original {
		t.Fatal("setup did not persist the new port and preserve board options and baud rate")
	}
}
