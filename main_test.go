package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestMain(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")
	tests := []struct {
		name         string
		opts         options
		files        map[string]string
		changedFiles []string
		stdout       []string
		err          string
	}{
		{
			name: "one file",
			opts: options{
				format:  "text",
				baseRef: "$baseRef",
				headRef: "$headRef",
			},
			files: map[string]string{
				"CODENOTIFY": "**/*.md @markdown",
				"file.md":    "",
			},
			changedFiles: []string{
				"file.md",
			},
			stdout: []string{
				"$baseRef...$headRef",
				"@markdown -> file.md",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			gitroot, err := ioutil.TempDir("", "codenotify")
			if err != nil {
				t.Fatalf("unable to create temporary directory: %s", err)
			}
			defer os.RemoveAll(gitroot)

			if err := os.Chdir(gitroot); err != nil {
				t.Fatalf("unable to change working directory to %s: %s", gitroot, err)
			}

			for file, content := range test.files {
				dir := filepath.Dir(file)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatalf("unable to make directory %s: %s", dir, err)
				}

				if err := ioutil.WriteFile(file, []byte(content), 0666); err != nil {
					t.Fatalf("unable to write file %s: %s", file, err)
				}
			}

			if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
				t.Fatalf("unable to git init: %s\n%s", err, string(out))
			}

			if out, err := exec.Command("git", "add", ".").CombinedOutput(); err != nil {
				t.Fatalf("unable to git add: %s\n%s", err, string(out))
			}

			if out, err := exec.Command("git", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "init").CombinedOutput(); err != nil {
				t.Fatalf("unable to make first commit: %s\n%s", err, string(out))
			}

			br, err := exec.Command("git", "rev-parse", "--short", "HEAD").CombinedOutput()
			if err != nil {
				t.Fatalf("unable to git rev-parse: %s\n%s", err, string(br))
			}

			for _, file := range test.changedFiles {
				// Easiest way to change a file is to remove it
				if out, err := exec.Command("git", "rm", file).CombinedOutput(); err != nil {
					t.Fatalf("unable to git rm: %s\n%s", err, string(out))
				}
			}

			if out, err := exec.Command("git", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "headRev").CombinedOutput(); err != nil {
				t.Fatalf("unable to git commit: %s\n%s", err, string(out))
			}

			hr, err := exec.Command("git", "rev-parse", "--short", "HEAD").CombinedOutput()
			if err != nil {
				t.Fatalf("unable to git rev-parse: %s\n%s", err, string(hr))
			}

			stdout := &bytes.Buffer{}

			baseRef := strings.TrimSpace(string(br))
			headRef := strings.TrimSpace(string(hr))
			err = testableMain(stdout, []string{
				"-cwd", gitroot,
				"-baseRef", baseRef,
				"-headRef", headRef,
				"-format", test.opts.format,
			})

			switch {
			case err != nil && test.err == "":
				t.Errorf("expected nil error; got %s", err)
			case err == nil && test.err != "":
				t.Errorf("expected error %q; got nil", test.err)
			}

			expectedStdout := joinLines(test.stdout)
			expectedStdout = strings.ReplaceAll(expectedStdout, "$baseRef", baseRef)
			expectedStdout = strings.ReplaceAll(expectedStdout, "$headRef", headRef)
			if stdout.String() != expectedStdout {
				t.Errorf("want stdout:\n%s\ngot:\n%s", expectedStdout, stdout.String())
			}
		})
	}
}

func TestWriteNotifications(t *testing.T) {
	tests := []struct {
		name   string
		opts   options
		notifs map[string][]string
		err    string
		output []string
	}{
		{
			name: "empty markdown",
			opts: options{
				filename: "CODENOTIFY",
				format:   "markdown",
				baseRef:  "a",
				headRef:  "b",
			},
			notifs: nil,
			output: []string{
				"<!-- codenotify:CODENOTIFY report -->",
				"[Codenotify](https://github.com/sourcegraph/codenotify): Notifying subscribers in CODENOTIFY files for diff a...b.",
				"",
				"No notifications.",
			},
		},
		{
			name: "empty text",
			opts: options{
				filename: "CODENOTIFY",
				format:   "text",
				baseRef:  "a",
				headRef:  "b",
			},
			notifs: nil,
			output: []string{
				"a...b",
				"No notifications.",
			},
		},
		{
			name: "markdown",
			opts: options{
				filename: "CODENOTIFY",
				format:   "markdown",
				baseRef:  "a",
				headRef:  "b",
			},
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
				"@js": {"file.js", "dir/file.js"},
			},
			output: []string{
				"<!-- codenotify:CODENOTIFY report -->",
				"[Codenotify](https://github.com/sourcegraph/codenotify): Notifying subscribers in CODENOTIFY files for diff a...b.",
				"",
				"| Notify | File(s) |",
				"|-|-|",
				"| @go | file.go<br>dir/file.go |",
				"| @js | file.js<br>dir/file.js |",
			},
		},
		{
			name: "text",
			opts: options{
				filename: "CODENOTIFY",
				format:   "text",
				baseRef:  "a",
				headRef:  "b",
			},
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
				"@js": {"file.js", "dir/file.js"},
			},
			output: []string{
				"a...b",
				"@go -> file.go, dir/file.go",
				"@js -> file.js, dir/file.js",
			},
		},
		{
			name: "unsupported format",
			opts: options{
				format: "pdf",
			},
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
			},
			err: "unsupported format: pdf",
		},
		{
			name: "exceeded subscriber threshold",
			opts: options{
				subscriberThreshold: 1,
			},
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
				"@js": {"file.js", "dir/file.js"},
			},
			output: []string{
				"Not notifying subscribers because the number of notifying subscribers (2) has exceeded the threshold (1).",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualOutput := bytes.Buffer{}
			err := test.opts.writeNotifications(&actualOutput, test.notifs)
			switch {
			case err != nil && test.err == "":
				t.Errorf("expected nil error; got %s", err)
			case err == nil && test.err != "":
				t.Errorf("expected error %q; got nil", test.err)
			}

			expectedOutput := joinLines(test.output)
			if expectedOutput != actualOutput.String() {
				t.Errorf("\nwant: %q\n got: %q", expectedOutput, actualOutput.String())
			}
		})
	}
}

func joinLines(lines []string) string {
	joined := strings.Join(lines, "\n")
	if joined == "" {
		return joined
	}
	return joined + "\n"
}

func TestNotifications(t *testing.T) {
	tests := []struct {
		name          string
		filename      string
		fs            memfs
		notifications map[string][]string
	}{
		{
			name:     "no notifications",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "nomatch.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: nil,
		},
		{
			name:     "file.md",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "file.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md"},
			},
		},
		{
			name:     "no leading slash",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "/file.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: nil,
		},
		{
			name:     "whitespace",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "\n\nfile.md @notify\n\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md"},
			},
		},
		{
			name:     "comments",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY": "#comment\n" +
					"file.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md"},
			},
		},
		{
			name:     "*",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "* @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"CODENOTIFY", "file.md"},
			},
		},
		{
			name:     "dir/*",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "dir/* @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"dir/file.md"},
			},
		},
		{
			name:     "**",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "** @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"CODENOTIFY", "file.md", "dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "**/*", // same as **
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "**/* @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"CODENOTIFY", "file.md", "dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "**/file.md",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "**/file.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md", "dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "dir/**",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "dir/** @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "dir/", // same as "dir/**"
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "dir/ @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "dir/**/file.md",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY":      "dir/**/file.md @notify\n",
				"file.md":         "",
				"dirfile.md":      "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"dir/file.md", "dir/dir/file.md"},
			},
		},
		{
			name:     "multiple subscribers",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY": "* @alice @bob\n",
				"file.md":    "",
			},
			notifications: map[string][]string{
				"@alice": {"CODENOTIFY", "file.md"},
				"@bob":   {"CODENOTIFY", "file.md"},
			},
		},
		{
			name:     "..",
			filename: "CODENOTIFY",
			fs: memfs{
				"dir/CODENOTIFY": "../* @alice @bob\n",
				"file.md":        "",
			},
			notifications: nil,
		},
		{
			name:     "multiple CODENOTIFY",
			filename: "CODENOTIFY",
			fs: memfs{
				"CODENOTIFY": "\n" +
					"* @rootany\n" +
					"*.go @rootgo\n" +
					"*.js @rootjs\n" +
					"**/* @all\n" +
					"**/*.go @allgo\n" +
					"**/*.js @alljs\n",
				"file.md": "",
				"file.js": "",
				"file.go": "",
				"dir/CODENOTIFY": "\n" +
					"* @dir/any\n" +
					"*.go @dir/go\n" +
					"*.js @dir/js\n" +
					"**/* @dir/all\n" +
					"**/*.go @dir/allgo\n" +
					"**/*.js @dir/alljs\n",
				"dir/file.md": "",
				"dir/file.go": "",
				"dir/file.js": "",
				"dir/dir/CODENOTIFY": "\n" +
					"* @dir/dir/any\n" +
					"*.go @dir/dir/go\n" +
					"*.js @dir/dir/js\n" +
					"**/* @dir/dir/all\n" +
					"**/*.go @dir/dir/allgo\n" +
					"**/*.js @dir/dir/alljs\n",
				"dir/dir/file.md": "",
				"dir/dir/file.go": "",
				"dir/dir/file.js": "",
			},
			notifications: map[string][]string{
				"@all": {
					"CODENOTIFY",
					"file.md",
					"file.js",
					"file.go",
					"dir/CODENOTIFY",
					"dir/file.md",
					"dir/file.go",
					"dir/file.js",
					"dir/dir/CODENOTIFY",
					"dir/dir/file.md",
					"dir/dir/file.go",
					"dir/dir/file.js",
				},
				"@allgo": {
					"file.go",
					"dir/file.go",
					"dir/dir/file.go",
				},
				"@alljs": {
					"file.js",
					"dir/file.js",
					"dir/dir/file.js",
				},
				"@rootany": {
					"CODENOTIFY",
					"file.md",
					"file.js",
					"file.go",
				},
				"@rootgo": {
					"file.go",
				},
				"@rootjs": {
					"file.js",
				},
				"@dir/all": {
					"dir/CODENOTIFY",
					"dir/file.md",
					"dir/file.go",
					"dir/file.js",
					"dir/dir/CODENOTIFY",
					"dir/dir/file.md",
					"dir/dir/file.go",
					"dir/dir/file.js",
				},
				"@dir/allgo": {
					"dir/file.go",
					"dir/dir/file.go",
				},
				"@dir/alljs": {
					"dir/file.js",
					"dir/dir/file.js",
				},
				"@dir/any": {
					"dir/CODENOTIFY",
					"dir/file.md",
					"dir/file.js",
					"dir/file.go",
				},
				"@dir/go": {
					"dir/file.go",
				},
				"@dir/js": {
					"dir/file.js",
				},
				"@dir/dir/all": {
					"dir/dir/CODENOTIFY",
					"dir/dir/file.md",
					"dir/dir/file.go",
					"dir/dir/file.js",
				},
				"@dir/dir/allgo": {
					"dir/dir/file.go",
				},
				"@dir/dir/alljs": {
					"dir/dir/file.js",
				},
				"@dir/dir/any": {
					"dir/dir/CODENOTIFY",
					"dir/dir/file.md",
					"dir/dir/file.js",
					"dir/dir/file.go",
				},
				"@dir/dir/go": {
					"dir/dir/file.go",
				},
				"@dir/dir/js": {
					"dir/dir/file.js",
				},
			},
		},
		{
			name:     "no notifications for OWNERS",
			filename: "OWNERS",
			fs: memfs{
				"CODENOTIFY":      "file.md @notify\n",
				"OWNERS":          "nomatch.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: nil,
		},
		{
			name:     "file.md in OWNERS",
			filename: "OWNERS",
			fs: memfs{
				"CODENOTIFY":      "nomatch.md @notify\n",
				"OWNERS":          "file.md @notify\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifs, err := notifications(test.fs, test.fs.paths(), test.filename)
			if err != nil {
				t.Errorf("expected nil error; got %s", err)
			}

			subs := map[string]struct{}{}
			for subscriber, actualfiles := range notifs {
				subs[subscriber] = struct{}{}
				expectedfiles := test.notifications[subscriber]
				sort.Strings(expectedfiles)
				sort.Strings(actualfiles)
				if !reflect.DeepEqual(expectedfiles, actualfiles) {
					t.Errorf("%s expected notifications for %v; got %v", subscriber, expectedfiles, actualfiles)
				}
			}

			for subscriber, expectedfiles := range test.notifications {
				if _, ok := subs[subscriber]; ok {
					// avoid duplicate errors
					continue
				}
				actualfiles := notifs[subscriber]
				sort.Strings(expectedfiles)
				sort.Strings(actualfiles)
				if !reflect.DeepEqual(expectedfiles, actualfiles) {
					t.Errorf("%s expected notifications for %v; got %v", subscriber, expectedfiles, actualfiles)
				}
			}
		})
	}
}

func TestIsRateLimitErr(t *testing.T) {
	cases := []struct {
		err      error
		expected bool
	}{
		{
			err:      fmt.Errorf("graphql: API rate limit exceeded for user ID 12345"),
			expected: true,
		}, {
			err:      nil,
			expected: false,
		}, {
			err:      fmt.Errorf("fake top error"),
			expected: false,
		}, {
			err:      fmt.Errorf("something something: API rate limit exceeded for user ID 12345"),
			expected: true,
		},
	}

	for _, tc := range cases {
		if isRateLimitErr(tc.err) != tc.expected {
			t.Errorf("expected %v got %v for %s", tc.expected, !tc.expected, tc.err)
		}
	}
}

// memfs is an in-memory implementation of the FS interface.
type memfs map[string]string

func (m memfs) paths() []string {
	paths := []string{}
	for path := range m {
		paths = append(paths, path)
	}
	return paths
}

func (m memfs) Open(name string) (File, error) {
	content, ok := m[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	mf := memfile{
		Buffer: bytes.NewBufferString(content),
	}

	return mf, nil
}
