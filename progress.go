package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	maxStageLog      = 512 * 1024
	discoveryTimeout = 30 * time.Second
)

var errStalledDiscovery = errors.New("library discovery stopped responding")

type progress struct {
	mu                          sync.Mutex
	log, partial                []byte
	truncated                   bool
	phase, detail               string
	lastOutput                  time.Time
	libraryBuilt, libraryReused int
}

func (p *progress) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lastOutput = time.Now()
	p.log = append(p.log, data...)
	if len(p.log) > maxStageLog {
		p.log = append(p.log[:0], p.log[len(p.log)-maxStageLog:]...)
		p.truncated = true
	}
	remaining := data
	for len(remaining) > 0 {
		i := bytes.IndexAny(remaining, "\r\n")
		if i < 0 {
			p.partial = append(p.partial, remaining...)
			break
		}
		p.partial = append(p.partial, remaining[:i]...)
		p.readLine(strings.TrimSpace(string(p.partial)))
		p.partial = p.partial[:0]
		remaining = remaining[i+1:]
	}
	if len(p.partial) > maxStageLog {
		p.partial = p.partial[:0]
	}
	return len(data), nil
}

func (p *progress) readLine(line string) {
	if path, found := strings.CutPrefix(line, "Using previously compiled file: "); found && isLibraryObject(path) {
		p.libraryReused++
	} else if _, target, found := strings.Cut(line, " -o "); found && strings.Contains(line, " -c ") && isLibraryObject(target) {
		p.libraryBuilt++
	}
	var phase string
	switch {
	case strings.HasPrefix(line, "Detecting libraries used"):
		phase = "Resolving libraries"
	case strings.HasPrefix(line, "Generating function prototypes"):
		phase = "Generating function prototypes"
	case strings.HasPrefix(line, "Compiling sketch"):
		phase = "Building sketch"
	case strings.HasPrefix(line, "Compiling libraries"):
		phase = "Checking library objects"
	case strings.HasPrefix(line, "Compiling core"):
		phase = "Checking core objects"
	case strings.HasPrefix(line, "Linking everything"):
		phase = "Linking firmware"
	}
	if phase != "" {
		p.phase, p.detail = phase, ""
	}
	if strings.HasPrefix(line, "ResolveLibrary(") && p.phase == "Resolving libraries" {
		p.detail = strings.TrimSuffix(strings.TrimPrefix(line, "ResolveLibrary("), ")")
	}
	if strings.HasPrefix(line, "Compiling library ") && p.phase == "Checking library objects" {
		p.detail = strings.Trim(strings.TrimPrefix(line, "Compiling library "), "\"")
	}
	if strings.HasPrefix(line, "Sketch uses ") || strings.HasPrefix(line, "Global variables use ") {
		p.phase, p.detail = "Checking firmware size", ""
	}
	if p.phase == "" && line != "" && !strings.HasPrefix(line, "FQBN:") && !strings.HasPrefix(line, "Using ") {
		p.detail = line
	}
}

func (p *progress) status() (phase, detail string, lastOutput time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	detail = p.detail
	if p.phase == "Checking library objects" && p.libraryBuilt+p.libraryReused > 0 {
		counts := fmt.Sprintf("%d compiled, %d reused", p.libraryBuilt, p.libraryReused)
		if detail != "" {
			counts = detail + " · " + counts
		}
		detail = counts
	}
	return p.phase, detail, p.lastOutput
}

func isLibraryObject(target string) bool {
	target = strings.Trim(strings.ReplaceAll(target, "\\", "/"), "\"")
	return strings.Contains(target, "/libraries/") && strings.HasSuffix(target, ".o")
}

func (p *progress) librarySummary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.libraryBuilt+p.libraryReused == 0 {
		return ""
	}
	return fmt.Sprintf("Library objects: %d compiled, %d reused\n\n", p.libraryBuilt, p.libraryReused)
}

func (p *progress) output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	log := strings.TrimSpace(string(p.log))
	if p.truncated {
		return "[Start of log omitted]\n" + log
	}
	return log
}

// Put the compiler's memory report above the verbose console output.
func buildSummary(log string) string {
	var lines []string
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "Sketch uses ") || strings.HasPrefix(line, "Global variables use ") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// Keep messages and diagnostics while hiding the repetitive command lines in the default view.
func compactBuildLog(log string) string {
	var out strings.Builder
	for line := range strings.SplitSeq(log, "\n") {
		if strings.HasPrefix(line, "Using previously compiled file:") ||
			strings.HasPrefix(line, "Using cached library dependencies for file:") {
			continue
		}
		start := strings.TrimPrefix(line, "\"")
		absolute := strings.HasPrefix(start, "/") || (len(start) >= 3 && start[1] == ':' && (start[2] == '\\' || start[2] == '/'))
		if absolute && strings.Contains(start, " -") &&
			!strings.Contains(start, ": error:") && !strings.Contains(start, ": warning:") &&
			!strings.Contains(start, ": fatal error:") && !strings.Contains(start, ": note:") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Compiling libraries"):
			line = "Checking library objects…"
		case strings.HasPrefix(line, "Compiling library "):
			line = "  " + strings.TrimPrefix(line, "Compiling library ")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}
