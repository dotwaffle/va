package main

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/mod/module"
)

func main() {
	// Convert embedded lists into links.
	links, err := fsToLinks(listfs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// If no path is provided, print registered links.
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, "ERROR: No supplied path.\n\n")
		fmt.Fprint(os.Stderr, "Registered short paths:\n\n")
		w := tabwriter.NewWriter(os.Stderr, 1, 4, 2, ' ', 0)
		keys := make([]string, 0, len(links))
		for k := range links {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			desc := links[k].Desc
			if desc != "" {
				// Make descriptions prettier.
				desc = "(" + desc + ")"
			}
			fmt.Fprintf(w, "%s\t=>\t%s %s\n", links[k].Short, links[k].Pkg, desc)
		}
		w.Flush()
		fmt.Fprint(os.Stderr, "\n")
		os.Exit(1)
	}

	// Lookup the path to see if it is a shortened link.
	mod := os.Args[1]
	modPath := strings.Split(mod, "@")
	if link, ok := links[modPath[0]]; ok {
		modLink := strings.Split(link.Pkg, "@")
		modPath[0] = modLink[0]
		// No version specified? Take the version from the link. The
		// user-specified version is always preferred over the
		// version specified in the shortened version.
		if len(modPath) == 1 {
			modPath = append(modPath, modLink[1])
		}
	}
	mod = strings.Join(modPath, "@")

	// Ensure we actually have a valid module path.
	if !validateMod(mod) {
		fmt.Fprintf(os.Stderr, "invalid pkg: %s (must be path@version)\n", mod)
		os.Exit(1)
	}

	// With a valid module, download it, then build it.
	toolDir, err := Download(mod)
	if err != nil {
		fmt.Fprintf(os.Stderr, "download: %v\n", err)
		os.Exit(1)
	}
	tool, err := Build(toolDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tool) // Remove the binary once we are done with it.

	// Run the freshly built binary.
	cmd := exec.Command(tool, os.Args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			fmt.Fprintf(os.Stderr, "va: %v\n", err)
			os.Exit(cmd.ProcessState.ExitCode())
		}
	}
}

// Link defines a shortened link.
type Link struct {
	Short string
	Pkg   string
	Desc  string
}

//go:embed lists/*.list
var listfs embed.FS

// fsToLinks converts an embedded filesystem into a map of shortened links.
func fsToLinks(f fs.FS) (map[string]Link, error) {
	links := make(map[string]Link)

	fsWalker := func(path string, d fs.DirEntry, errWalker error) error {
		// Skip directories, needs to be a file.
		if d.IsDir() {
			return nil
		}

		// Strip the embedded filesystem prefix and extension suffix.
		name := strings.TrimPrefix(path, "lists/")
		if !strings.HasSuffix(name, ".list") {
			// If the suffix is not present, skip over the file.
			return nil
		}
		name = strings.TrimSuffix(name, ".list")

		if name == "_" {
			// "_" is a special name meaning "no prefix".
			name = ""
		} else {
			// otherwise, use the filename as the prefix.
			name = name + "/"
		}

		// Read the file to get the shortenings.
		list, err := f.Open(path)
		if err != nil {
			return err
		}
		defer list.Close()
		scanner := bufio.NewScanner(list)
		for scanner.Scan() {
			link, err := lineToLink(scanner.Text())
			if err != nil {
				return err
			}

			// Skip empty links.
			if link == (Link{}) {
				continue
			}

			// Rewrite the short name with any prefix.
			link.Short = name + link.Short

			// Ensure the link has not already been seen, then add it.
			if _, ok := links[link.Short]; ok {
				return fmt.Errorf("link %s already exists, file: %s", link.Short, path)
			}
			links[link.Short] = link
		}
		return nil
	}

	if err := fs.WalkDir(f, ".", fsWalker); err != nil {
		return links, err
	}
	return links, nil
}

// lineToLink converts a line of text into a Link.
func lineToLink(line string) (Link, error) {
	if strings.HasPrefix(line, "#") {
		// Ignore line, it is a comment.
		return Link{}, nil
	}
	split := strings.Split(line, " ")
	if len(split) < 2 {
		return Link{}, errors.New("bad line")
	}
	short, pkg, desc := split[0], split[1], strings.Join(split[2:], " ")
	if !validateShort(short) || !validateMod(pkg) {
		return Link{}, fmt.Errorf("bad module: %s %s", short, pkg)
	}
	return Link{
		Short: short,
		Pkg:   pkg,
		Desc:  desc,
	}, nil

}

var (
	reShort = regexp.MustCompile(`^([0-9A-Za-z]+[0-9A-Za-z_-]*[0-9A-Za-z]+)|([0-9A-Za-z]+)$`)
)

// validateShort validates a short name, to ensure it starts and ends with an
// alphanumeric character, and optionally has underscores or dashes in the
// middle of it.
func validateShort(short string) bool {
	return reShort.MatchString(short)
}

// validateMod takes a module name and ensures it is a valid Go module name.
func validateMod(mod string) bool {
	split := strings.Split(mod, "@")
	if len(split) != 2 {
		// For module mode, must specify a version.
		return false
	}
	if err := module.CheckPath(split[0]); err != nil {
		// Must be a valid module path.
		return false
	}

	// LGTM.
	return true
}
