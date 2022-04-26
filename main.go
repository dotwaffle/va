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
			fmt.Fprintf(w, "%s\t=>\t%s\t(%s)\n", links[k].Short, links[k].Pkg, links[k].Desc)
		}
		w.Flush()
		fmt.Fprint(os.Stderr, "\n")
		os.Exit(1)
	}

	// Lookup the path to see if it is a shortened link.
	pkg := os.Args[1]
	pkgPath := strings.Split(pkg, "@")
	if link, ok := links[pkgPath[0]]; ok {
		pkgLink := strings.Split(link.Pkg, "@")
		pkgPath[0] = pkgLink[0]
		// No version specified? Take the version from the link. The
		// user-specified version is always preferred over the
		// version specified in the shortened version.
		if len(pkgPath) == 1 {
			pkgPath = append(pkgPath, pkgLink[1])
		}
	}
	pkg = strings.Join(pkgPath, "@")

	// Ensure we actually have a valid module path.
	if !validatePkg(pkg) {
		fmt.Fprintf(os.Stderr, "invalid pkg: %s (must be path@version)\n", pkg)
		os.Exit(1)
	}

	// Construct the command line, and run it.
	run := []string{"run", pkg}
	run = append(run, os.Args[2:]...)
	cmd := exec.Command("go", run...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "va: %v", err)
		os.Exit(1)
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
				return nil
			}

			// Ensure the link has not already been seen, then add it.
			fullName := name + link.Short
			if _, ok := links[fullName]; ok {
				return fmt.Errorf("link %s already exists, file: %s", fullName, path)
			}
			links[fullName] = link
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
	if !validateShort(short) || !validatePkg(pkg) {
		return Link{}, fmt.Errorf("bad package: %s %s", short, pkg)
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

// validatePkg takes a package name and ensures it is a valid Go module name.
func validatePkg(pkg string) bool {
	split := strings.Split(pkg, "@")
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
