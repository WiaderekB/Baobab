package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Each sketch and complete FQBN get their own incremental build.
func openBuildCache(sketch, board string) (string, func(), error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", nil, err
	}
	key := sha256.Sum256([]byte(sketch + "\x00" + board))
	dir := filepath.Join(cache, "baobab", "builds", fmt.Sprintf("%x", key))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	// A stable physical path keeps compiler dependency files valid on macOS.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", nil, err
	}
	// The lock lives beside the build directory so --clean cannot remove it.
	return lockBuildCache(dir)
}
