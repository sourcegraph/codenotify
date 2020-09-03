package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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
	parts := strings.Split(path, string(os.PathSeparator))

	subscribers := []string{}
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
			fields := strings.Fields(rule)
			if len(fields) < 2 {
				return nil, fmt.Errorf("expected at least two fields for rule in %s: %s", rulefilepath, rule)
			}

			rel, err := filepath.Rel(base, path)
			if err != nil {
				return nil, err
			}

			matched, err := filepath.Match(fields[0], rel)
			if err != nil {
				return nil, fmt.Errorf("invalid match pattern in %s: %s ", rulefilepath, rule)
			}

			if matched {
				subscribers = append(subscribers, fields[1:]...)
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}

	}

	return subscribers, nil
}
