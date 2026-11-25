//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestStalledDiscoveryRetriesBuildFromScratch(t *testing.T) {
	dir := t.TempDir()
	fakeCLI := `#!/bin/sh
for arg do
	if [ "$arg" = "--clean" ]; then
		echo "Sketch uses 100 bytes"
		exit 0
	fi
done
echo "Detecting libraries used..."
exec sleep 10
`
	if err := os.WriteFile(filepath.Join(dir, "arduino-cli"), []byte(fakeCLI), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &model{
		mode: busy, start: time.Now(), retryDiscovery: true,
		stages: buildStages(dir, profile{Board: "arduino:avr:uno"}, dir, false, false),
	}
	cmd := m.nextStage()
	t.Cleanup(func() {
		if m.cancel != nil {
			m.cancel(nil)
		}
	})
	finished := make(chan tea.Msg, 1)
	go func() { finished <- cmd() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		phase, _, _ := m.progress.status()
		if phase == "Resolving libraries" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not receive live discovery status from CLI")
		}
		time.Sleep(5 * time.Millisecond)
	}

	m.progress.mu.Lock()
	m.progress.lastOutput = time.Now().Add(-discoveryTimeout)
	m.progress.mu.Unlock()
	m.Update(tick{m.jobID})

	select {
	case msg := <-finished:
		_, retry := m.Update(msg)
		if retry == nil {
			t.Fatalf("build didn't retry, report: %s", m.log)
		}
		m.Update(retry())
	case <-time.After(3 * time.Second):
		t.Fatal("stalled compiler did not stop")
	}
	if m.mode != report || !strings.HasPrefix(m.title, "Done") || !strings.Contains(m.log, "Sketch uses 100 bytes") {
		t.Fatalf("clean retry didn't complete: %s\n%s", m.title, m.log)
	}
}

func TestCancellationDoesNotAdvanceToUpload(t *testing.T) {
	m := &model{
		mode: busy, cancelRequested: true,
		stages: buildStages("Display", profile{Board: "arduino:avr:uno", Port: "/dev/test"}, "output", true, false),
	}
	_, next := m.Update(stageDone{})
	if next != nil || m.mode != report || m.title != "Canceled" {
		t.Fatal("a queued compile result must not trigger upload after cancellation")
	}
}
