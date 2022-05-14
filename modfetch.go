package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Download goes out and downloads the module requested to the usual module cache location.
func Download(mod string) (dir string, err error) {
	// Split out the path and version from the module.
	split := strings.Split(mod, "@")
	if len(split) != 2 {
		// For module mode, must specify a version.
		return "", fmt.Errorf("not a module")
	}
	path := split[0]
	version := split[1]

	// The "tail" can be thought of like this:
	// example.com/a/b/cmd/d@latest
	// The module is at example.com/a/b so trying to get that will fail.
	// Therefore we split it into example.com/a/b/cmd@latest and keep "d"
	// in the "tail" which we will add to the module directory later.
	// "example.com/a/b" will be the path, "cmd/d" will be the tail, and
	// "latest" will be the version.
	tail := ""
	var out []byte
	found := false
	for !found {
		// Reconstitute the module string, and download it.
		pathVersion := path + "@" + version
		out, err = exec.Command("go", "mod", "download", "-json", pathVersion).CombinedOutput()
		if err != nil {
			path, tail = pathTrim(path, tail)
			if path == "." {
				// The command failed all the way up to the root.
				return "", fmt.Errorf("mod-download: %w", err)
			}
			// The command failed, assume it was because the path
			// was not where a module was located, and ascend the
			// path tree to try again elsewhere.
			continue
		}
		// We got what we were looking for, so stop looking.
		found = true
	}

	// From the output of "go mod download" we can extract the information
	// about where the unpacked module can be found.
	modinfo := packages.Module{}
	if err := json.Unmarshal(out, &modinfo); err != nil {
		return "", fmt.Errorf("json: %w", err)
	}

	// Construct the full package directory for the tool we are building.
	dir = filepath.Join(modinfo.Dir, tail)

	return dir, nil
}

// pathTrim chops off the last part of the path, prepends it onto the tail,
// and returns the new path and tail values to the caller.
func pathTrim(curPath, curTail string) (newPath, newTail string) {
	newPath = path.Clean(path.Dir(curPath))
	newTail = path.Join(path.Base(curPath), curTail)
	return newPath, newTail
}

// Build changes to where the module has been unpacked to, and builds it
// into a temporary file. It is the caller's responsibility to remove
// the temporary file once they have finished with it.
func Build(dir string) (cmdPath string, err error) {
	toolName := filepath.Base(dir)
	tmpFile, err := os.CreateTemp("", toolName)
	if err != nil {
		return "", err
	}

	// We actually only want the filename, we can just take that and
	// then close the file since we are going to clobber it anyway.
	tmpFileName := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	// Build the tool in the place it was downloaded, dropping it
	// in the temporary location we discovered earlier.
	cmd := exec.Command("go", "build", "-v", "-o", tmpFileName)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmpFileName)
		return "", err
	}
	return tmpFileName, nil
}
