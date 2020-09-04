package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	// TODO!
	// os.Exit(testableMain(os.Args[1:], os.Stdin))
}

// func testableMain(args []string, stdin io.Reader) int {
// 	scanner := bufio.NewScanner(stdin)
// 	for scanner.Scan() {
// 		fmt.Println(scanner.Text())
// 	}

// 	if err := scanner.Err(); err != nil {
// 		log.Println(err)
// 		return 1
// 	}

// 	return 0
// }

type FS interface {
	Open(name string) (File, error)
}

type File interface {
	Stat() (os.FileInfo, error)
	Read([]byte) (int, error)
	Close() error
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
