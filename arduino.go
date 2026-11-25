package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func isSketch(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, filepath.Base(dir)+".ino"))
	return err == nil && info.Mode().IsRegular()
}

func startPath(path string) (root, sketch string, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		path = filepath.Dir(path)
	}
	for dir := path; ; dir = filepath.Dir(dir) {
		if isSketch(dir) {
			return dir, dir, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return path, "", nil
}

type item struct{ label, detail, value, shortcut string }

func sketches(ctx context.Context, root string) ([]item, error) {
	var items []item
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "build") {
			return filepath.SkipDir
		}
		if isSketch(path) {
			rel, _ := filepath.Rel(root, path)
			items = append(items, entry(filepath.Base(path), rel, path))
			return filepath.SkipDir
		}
		return nil
	})
	return items, err
}

func arduinoCommand(ctx context.Context, args ...string) *exec.Cmd {
	return useEnglish(backgroundCommand(ctx, "arduino-cli", args...))
}

// Keep progress markers predictable and compiler messages in English.
func useEnglish(cmd *exec.Cmd) *exec.Cmd {
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C", "LANGUAGE=en")
	return cmd
}

func cliJSON(ctx context.Context, dest any, args ...string) error {
	cmd := arduinoCommand(ctx, append([]string{"--json"}, args...)...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var e *exec.ExitError
		if errors.As(err, &e) {
			var result struct{ Error string }
			if json.Unmarshal(out, &result) == nil && result.Error != "" {
				return errors.New(result.Error)
			}
			if msg := strings.TrimSpace(string(e.Stderr)); msg != "" {
				return errors.New(msg)
			}
		}
		return err
	}
	return json.Unmarshal(out, dest)
}

func boards(ctx context.Context) ([]item, error) {
	var data struct{ Boards []struct{ Name, FQBN string } }
	err := cliJSON(ctx, &data, "board", "listall")
	var items []item
	for _, b := range data.Boards {
		items = append(items, entry(b.Name, b.FQBN, b.FQBN))
	}
	return items, err
}

func ports(ctx context.Context) ([]item, error) {
	var data struct {
		Detected []struct {
			Port     struct{ Address, Protocol string }
			Matching []struct{ Name string } `json:"matching_boards"`
		} `json:"detected_ports"`
	}
	err := cliJSON(ctx, &data, "board", "list")
	var items []item
	for _, d := range data.Detected {
		if d.Port.Protocol != "serial" || strings.Contains(d.Port.Address, "Bluetooth-Incoming-Port") || strings.HasSuffix(d.Port.Address, "/cu.debug-console") {
			continue
		}
		name := "Unknown board model"
		if len(d.Matching) > 0 {
			name = d.Matching[0].Name
		}
		items = append(items, entry(d.Port.Address, name, d.Port.Address))
	}
	return items, err
}

type platform struct {
	ID        string
	Installed string `json:"installed_version"`
	Latest    string `json:"latest_version"`
	Releases  map[string]struct{ Name string }
}

func packages(ctx context.Context, installed bool) ([]item, error) {
	var data struct{ Platforms []platform }
	action := "search"
	if installed {
		action = "list"
	}
	err := cliJSON(ctx, &data, "core", action)
	var items []item
	for _, p := range data.Platforms {
		version := p.Latest
		if installed {
			version = p.Installed
		}
		name := p.Releases[version].Name
		if name == "" {
			name = p.ID
		}
		items = append(items, entry(name, strings.TrimSpace(p.ID+" "+version), p.ID))
	}
	return items, err
}

func libraries(ctx context.Context, query string, installed bool) ([]item, error) {
	type lib struct{ Name, Author, Version, Location string }
	var data struct {
		Libraries []struct {
			Name   string
			Latest lib
		}
		Installed []struct{ Library lib } `json:"installed_libraries"`
	}
	args := []string{"lib", "list"}
	if !installed {
		args = append([]string{"lib", "search", "--omit-releases-details", "--"}, strings.Fields(query)...)
	}
	err := cliJSON(ctx, &data, args...)
	var items []item
	for _, l := range data.Libraries {
		name := l.Name
		if name == "" {
			name = l.Latest.Name
		}
		if name != "" {
			items = append(items, entry(name, strings.TrimSpace(l.Latest.Author+" "+l.Latest.Version), name))
		}
	}
	for _, installed := range data.Installed {
		l := installed.Library
		items = append(items, entry(l.Name, l.Version+" · "+l.Location, l.Name))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].label < items[j].label })
	return items, err
}

type stage struct {
	title   string
	args    []string
	timeout time.Duration
}

func buildStages(sketch string, p profile, output string, upload, clean bool) []stage {
	args := []string{"compile", "--verbose", "--fqbn", p.Board, "--build-path", output}
	title := "Building"
	if clean {
		args = append(args, "--clean")
		title = "Clean build"
	}
	stages := []stage{{title, append(args, sketch), 15 * time.Minute}}
	if upload {
		args := []string{"upload", "--fqbn", p.Board, "--port", p.Port, "--input-dir", output}
		if p.UploadSpeed > 0 {
			args = append(args, "--upload-property", fmt.Sprintf("upload.speed=%d", p.UploadSpeed))
		}
		stages = append(stages, stage{"Uploading", append(args, sketch), 90 * time.Second})
	}
	return stages
}

func runStage(ctx context.Context, s stage, progress *progress) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	cmd := arduinoCommand(ctx, s.args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = progress, progress
	err := cmd.Run()
	if cause := context.Cause(ctx); cause != nil {
		err = cause
	}
	if err != nil {
		err = fmt.Errorf("%s: %w", s.title, err)
	}
	return progress.output(), err
}

func diagnostics(ctx context.Context, root, sketch string, p profile) string {
	var out strings.Builder
	path, _ := exec.LookPath("arduino-cli")
	cmd := arduinoCommand(ctx, "version")
	cmd.WaitDelay = 2 * time.Second
	if data, err := cmd.Output(); err == nil {
		fmt.Fprintln(&out, strings.TrimSpace(string(data)))
	} else {
		fmt.Fprintln(&out, "Arduino CLI: "+err.Error())
	}
	fmt.Fprintln(&out, "Executable: "+path)
	fmt.Fprintln(&out, "Workspace: "+root)
	if sketch == "" {
		fmt.Fprintln(&out, "Sketch: choose a sketch in Setup or Sketch")
	} else {
		fmt.Fprintln(&out, "Sketch: "+sketch)
	}
	fmt.Fprintf(&out, "\nBoard: %s · %s baud\n", p.Board, p.Baud)
	fmt.Fprintln(&out, "Upload speed: "+uploadSpeedLabel(p.UploadSpeed))
	parts := strings.SplitN(p.Board, ":", 3)
	if len(parts) == 3 {
		core := strings.Join(parts[:2], ":")
		installed, err := packages(ctx, true)
		if err != nil {
			fmt.Fprintln(&out, "Could not list board packages: "+err.Error())
		} else {
			found := false
			for _, row := range installed {
				if row.value == core {
					fmt.Fprintln(&out, "Installed core: "+row.detail)
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintln(&out, "Missing core "+core+" · open Packages")
			}
		}
	}
	port := p.Port
	if port == "" {
		port = "choose a USB port before uploading"
	}
	fmt.Fprintln(&out, "\nSaved port: "+port)
	detected, err := ports(ctx)
	switch {
	case err != nil:
		fmt.Fprintln(&out, "Port detection: "+err.Error())
	case len(detected) == 0:
		fmt.Fprintln(&out, "No USB ports detected.")
	default:
		for _, row := range detected {
			fmt.Fprintln(&out, "  "+row.label+" · "+row.detail)
		}
	}
	if dir, err := os.UserCacheDir(); err == nil {
		fmt.Fprintln(&out, "\nBuild cache: "+filepath.Join(dir, "baobab", "builds"))
	}
	return strings.TrimSpace(out.String())
}
