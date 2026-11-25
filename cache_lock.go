//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockBuildCache(dir string) (string, func(), error) {
	release, err := lockFile(filepath.Join(dir, "lock"))
	if errors.Is(err, unix.EWOULDBLOCK) {
		err = errors.New("This sketch is already building or uploading in another window.")
	}
	return filepath.Join(dir, "output"), release, err
}

func lockPreferences(path string) (func(), error) {
	release, err := lockFile(path)
	if errors.Is(err, unix.EWOULDBLOCK) {
		err = errors.New("another Baobab window is saving settings; try again")
	}
	return release, err
}

func lockFile(path string) (func(), error) {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, err
	}
	return func() { lock.Close() }, nil
}
