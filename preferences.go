package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type profile struct {
	Board       string
	Port        string
	Baud        string
	UploadSpeed int `json:"UploadSpeed,omitempty"`
}

type preferences struct {
	Profiles      map[string]profile `json:"profiles"`
	LastSketch    map[string]string  `json:"lastSketch"`
	LastWorkspace string             `json:"lastWorkspace,omitempty"`
}

func preferencesPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "baobab", "profiles.json"), err
}

func readPreferences(path string) (preferences, error) {
	var settings preferences
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return settings, err
	default:
		if err := json.Unmarshal(data, &settings); err != nil {
			return settings, err
		}
		if settings.Profiles == nil {
			// Older versions stored the sketch-to-profile map directly.
			if err := json.Unmarshal(data, &settings.Profiles); err != nil {
				return settings, err
			}
		}
	}
	if settings.Profiles == nil {
		settings.Profiles = make(map[string]profile)
	}
	if settings.LastSketch == nil {
		settings.LastSketch = make(map[string]string)
	}
	return settings, nil
}

func saveSelection(path, root, sketch string, update *profile) (preferences, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return preferences{}, err
	}
	release, err := lockPreferences(path + ".lock")
	if err != nil {
		return preferences{}, err
	}
	defer release()

	settings, err := readPreferences(path)
	if err != nil {
		return preferences{}, err
	}
	if update == nil && settings.LastSketch[root] == sketch && settings.LastWorkspace == root {
		return settings, nil
	}
	settings.LastSketch[root] = sketch
	settings.LastWorkspace = root
	if update != nil {
		settings.Profiles[sketch] = *update
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return preferences{}, err
	}
	file, err := os.CreateTemp(dir, ".profiles-*")
	if err != nil {
		return preferences{}, err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return preferences{}, err
	}
	if closeErr != nil {
		return preferences{}, closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return preferences{}, err
	}
	return settings, nil
}

func (p preferences) rememberedSketch(root string) string {
	if sketch := p.LastSketch[root]; insideWorkspace(root, sketch) && isSketch(sketch) {
		return sketch
	}
	// Restore an older profile when only one existing sketch matches the workspace.
	var candidate string
	for sketch := range p.Profiles {
		if !insideWorkspace(root, sketch) || !isSketch(sketch) {
			continue
		}
		if candidate != "" {
			return ""
		}
		candidate = sketch
	}
	return candidate
}

func insideWorkspace(root, sketch string) bool {
	if sketch == "" {
		return false
	}
	rel, err := filepath.Rel(root, sketch)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
