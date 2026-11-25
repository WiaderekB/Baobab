package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestUploadSpeedPersistsAndOnlyChangesUpload(t *testing.T) {
	root := t.TempDir()
	sketch := filepath.Join(root, "Blink")
	settingsFile := filepath.Join(root, "profiles.json")
	settings, err := saveSelection(settingsFile, root, sketch, &profile{
		Board: "arduino:avr:uno", Port: "/dev/test", Baud: "9600",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &model{root: root, sketch: sketch, preferences: settings, preferencesFile: settingsFile}
	m.activate("upload-speed")
	m.choose(m.rows[len(m.rows)-1])
	m.query = "invalid"
	m.acceptText()
	if m.mode != input {
		t.Fatal("invalid upload speed was accepted")
	}
	m.query = "74880"
	m.acceptText()
	settings, err = readPreferences(settingsFile)
	if err != nil {
		t.Fatal(err)
	}
	p := settings.Profiles[sketch]
	if p.UploadSpeed != 74880 || p.Baud != "9600" {
		t.Fatalf("upload and monitor settings were not saved separately: %+v", p)
	}
	stages := buildStages(sketch, p, "output", true, false)
	if slices.Contains(stages[0].args, "--upload-property") ||
		!slices.Contains(stages[1].args, "upload.speed=74880") {
		t.Fatalf("speed should only override the upload stage: %+v", stages)
	}

	m.activate("upload-speed")
	if m.rows[m.cursor].value != "74880" {
		t.Fatal("saved custom speed should be preselected")
	}
	m.choose(m.rows[0])
	settings, err = readPreferences(settingsFile)
	if err != nil {
		t.Fatal(err)
	}
	stages = buildStages(sketch, settings.Profiles[sketch], "output", true, false)
	if slices.Contains(stages[1].args, "--upload-property") {
		t.Fatal("board default should omit the upload speed override")
	}
}
