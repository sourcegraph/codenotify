package main

import (
	"bytes"
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
	tests := []struct {
		name   string
		rev    string
		format string
		files  map[string]string
		stdin  []string
		stdout []string
		stderr []string
	}{
		{
			name:   "one file",
			rev:    "HEAD",
			format: "text",
			files: map[string]string{
				"CODENOTIFY": "**/*.md @markdown",
				"file.md":    "",
			},
			stdin: []string{
				"file.md",
			},
			stdout: []string{
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

			if out, err := exec.Command("git", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "'init'").CombinedOutput(); err != nil {
				t.Fatalf("unable to git commit: %s\n%s", err, string(out))
			}

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			testableMain(mainArgs{
				stdin:  bytes.NewBufferString(joinLines(test.stdin)),
				stdout: stdout,
				stderr: stderr,
				rev:    test.rev,
				format: test.format,
			})

			expectedStdout := joinLines(test.stdout)
			if stdout.String() != expectedStdout {
				t.Errorf("want stdout:\n%s\ngot:\n%s", expectedStdout, stdout.String())
			}

			expectedStderr := joinLines(test.stderr)
			if stderr.String() != expectedStderr {
				t.Errorf("want stderr:\n%s\ngot:\n%s", expectedStderr, stderr.String())
			}
		})
	}
}

func TestPrintNotifications(t *testing.T) {
	tests := []struct {
		name   string
		format string
		notifs map[string][]string
		err    string
		output []string
	}{
		{
			name:   "markdown",
			format: "markdown",
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
				"@js": {"file.js", "dir/file.js"},
			},
			output: []string{
				"# CODENOTIFY report",
				"",
				"| Notify | File(s) |",
				"|-|-|",
				"| @go | file.go, dir/file.go |",
				"| @js | file.js, dir/file.js |",
			},
		},
		{
			name:   "text",
			format: "text",
			notifs: map[string][]string{
				"@go": {"file.go", "dir/file.go"},
				"@js": {"file.js", "dir/file.js"},
			},
			output: []string{
				"@go -> file.go, dir/file.go",
				"@js -> file.js, dir/file.js",
			},
		},
		{
			name:   "unsupported format",
			format: "pdf",
			err:    "unsupported format: pdf",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualOutput := bytes.Buffer{}
			err := printNotifications(&actualOutput, test.format, test.notifs)
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
		fs            memfs
		notifications map[string][]string
	}{
		{
			name: "file.md",
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
			name: "whitespace",
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
			name: "comments",
			fs: memfs{
				"CODENOTIFY": "#comment\n" +
					"file.md @notify #comment\n",
				"file.md":         "",
				"dir/file.md":     "",
				"dir/dir/file.md": "",
			},
			notifications: map[string][]string{
				"@notify": {"file.md"},
			},
		},
		{
			name: "*",
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
			name: "dir/*",
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
			name: "**",
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
			name: "**/*", // same as **
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
			name: "**/file.md",
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
			name: "dir/**",
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
			name: "dir/", // same as "dir/**"
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
			name: "dir/**/file.md",
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
			name: "multiple subscribers",
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
			name: "multiple CODENOTIFY",
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifs, err := notifications(test.fs, test.fs.paths())
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
