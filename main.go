package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	rev := *flag.String("rev", "HEAD", "The revision of CODENOTIFY files to use. This is generally the base revision of a change.")
	format := *flag.String("format", "text", "The format of the output: text or markdown")

	flag.Parse()

	paths, err := readLines(os.Stdin)
	if err != nil {
		fmt.Println("error reading stdin:", err)
		os.Exit(1)
	}

	notifs, err := notifications(&gitfs{rev: rev}, paths)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := printNotifications(os.Stdout, format, notifs); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func printNotifications(w io.Writer, format string, notifs map[string][]string) error {
	subs := []string{}
	for sub := range notifs {
		subs = append(subs, sub)
	}
	sort.Strings(subs)

	switch format {
	case "text":
		for _, sub := range subs {
			files := notifs[sub]
			fmt.Fprintln(w, sub, "->", strings.Join(files, ", "))
		}
		return nil
	case "markdown":
		fmt.Fprintf(w, "# CODENOTIFY report\n\n")
		fmt.Fprintf(w, "| Notify | File(s) |\n")
		fmt.Fprintf(w, "|-|-|\n")
		for _, sub := range subs {
			files := notifs[sub]
			fmt.Fprintf(w, "| %s | %s |\n", sub, strings.Join(files, ", "))
		}
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func readLines(r io.Reader) ([]string, error) {
	lines := []string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func notifications(fs FS, paths []string) (map[string][]string, error) {
	notifications := map[string][]string{}
	for _, path := range paths {
		subs, err := subscribers(fs, path)
		if err != nil {
			return nil, err
		}

		for _, sub := range subs {
			notifications[sub] = append(notifications[sub], path)
		}
	}
	return notifications, nil

}

func subscribers(fs FS, path string) ([]string, error) {
	subscribers := []string{}

	parts := strings.Split(path, string(os.PathSeparator))
	for i := range parts {
		base := filepath.Join(parts[:i]...)
		rulefilepath := filepath.Join(base, "CODENOTIFY")

		rulefile, err := fs.Open(rulefilepath)
		if err != nil {
			if err == os.ErrNotExist {
				continue
			}
			return nil, err
		}

		scanner := bufio.NewScanner(rulefile)
		for scanner.Scan() {
			rule := scanner.Text()
			rule = trimComment(rule)

			fields := strings.Fields(rule)
			switch len(fields) {
			case 0:
				// skip blank line
				continue
			case 1:
				return nil, fmt.Errorf("expected at least two fields for rule in %s: %s", rulefilepath, rule)
			}

			rel, err := filepath.Rel(base, path)
			if err != nil {
				return nil, err
			}

			re, err := patternToRegexp(fields[0])
			if err != nil {
				return nil, fmt.Errorf("invalid pattern in %s: %s: %w", rulefilepath, rule, err)
			}

			if re.MatchString(rel) {
				subscribers = append(subscribers, fields[1:]...)
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	return subscribers, nil
}

func trimComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}
	return s
}

func patternToRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern[len(pattern)-1:] == "/" {
		pattern += "**"
	}
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, `/\*\*/`, "/([^/]*/)*")
	pattern = strings.ReplaceAll(pattern, `\*\*/`, "([^/]+/)*")
	pattern = strings.ReplaceAll(pattern, `/\*\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\*\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\*`, "[^/]*")
	pattern = "^" + pattern + "$"
	return regexp.Compile(pattern)
}
