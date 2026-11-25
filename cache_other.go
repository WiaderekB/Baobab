//go:build !darwin && !linux

package main

import "os"

func lockBuildCache(_ string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "baobab-build-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { os.RemoveAll(dir) }, nil
}

func lockPreferences(_ string) (func(), error) {
	return func() {}, nil
}
