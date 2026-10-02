package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
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

// fakeGitHub replaces only the HTTP boundary; Git and report generation stay real.
type fakeGitHub func(query string, variables map[string]string) (int, string)

func (f fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	var request struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}
	if req.Method == http.MethodGet {
		request.Query = "GET " + req.URL.Path
	} else if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return nil, err
	}
	status, body := f(request.Query, request.Variables)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: ioutil.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestGitHubNotificationsCurrentComparison(t *testing.T) {
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(dir, name, body string) {
		t.Helper()
		if err := ioutil.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) string {
		git(root, "add", ".")
		git(root, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", message)
		return git(root, "rev-parse", "HEAD")
	}
	git(root, "init")
	for _, filename := range []string{"CODENOTIFY", "OWNERS"} {
		write(root, filename, "intended.txt @old-subscriber\nunrelated.txt @unrelated\n")
	}
	write(root, "intended.txt", "before")
	write(root, "unrelated.txt", "before")
	oldBase := commit("old base")
	git(root, "branch", "old-base")
	write(root, "unrelated.txt", "unrelated change")
	for _, filename := range []string{"CODENOTIFY", "OWNERS"} {
		write(root, filename, "intended.txt @intended @author\nunrelated.txt @unrelated\n")
	}
	common := commit("shared changes")
	git(root, "checkout", "-b", "new-base")
	write(root, "base-only.txt", "base change")
	newBase := commit("new base")
	git(root, "checkout", "-b", "pr-head", common)
	write(root, "intended.txt", "intended change")
	head := commit("PR change")

	type state struct {
		BaseRef, BaseSHA, HeadSHA, State string
		Draft                            bool
	}
	old := state{BaseRef: "old-base", BaseSHA: oldBase, HeadSHA: head, State: "open"}
	current := state{BaseRef: "new-base", BaseSHA: newBase, HeadSHA: head, State: "open"}
	renamed := current
	renamed.BaseRef = "renamed-base"
	advanced := old
	advanced.BaseSHA = newBase
	replaced := current
	replaced.HeadSHA = newBase
	draft := current
	draft.Draft = true
	closed := current
	closed.State = "closed"
	empty := current
	empty.BaseSHA = head

	for _, tc := range []struct {
		name        string
		filename    string
		states      []state
		existing    bool
		wantReport  bool
		wantEmpty   bool
		wantLookups int
		wantError   string
		errorAt     int
		missingNode bool
	}{
		{name: "retry add", states: []state{old, current, current, current}, wantReport: true, wantLookups: 2},
		{name: "retry update", filename: "OWNERS", states: []state{old, current, current, current}, existing: true, wantReport: true, wantLookups: 2},
		{name: "live base replaces event base", states: []state{current, current}, wantReport: true, wantLookups: 1},
		{name: "base OID changes", states: []state{old, advanced, advanced, advanced}, wantReport: true, wantLookups: 2},
		{name: "base name changes", states: []state{current, renamed, renamed, renamed}, wantReport: true, wantLookups: 2},
		{name: "head replaced before calculation", states: []state{replaced}},
		{name: "draft before calculation", states: []state{draft}},
		{name: "closed before calculation", states: []state{closed}},
		{name: "head replaced before add", states: []state{old, replaced}, wantLookups: 1},
		{name: "head replaced before update", states: []state{old, replaced}, existing: true, wantLookups: 1},
		{name: "draft before add", states: []state{old, draft}, wantLookups: 1},
		{name: "draft before update", states: []state{old, draft}, existing: true, wantLookups: 1},
		{name: "closed before add", states: []state{old, closed}, wantLookups: 1},
		{name: "closed before update", states: []state{old, closed}, existing: true, wantLookups: 1},
		{name: "initial refresh error", states: []state{old}, errorAt: 1, wantError: "refresh failed"},
		{name: "final refresh error", states: []state{old, current}, errorAt: 2, existing: true, wantLookups: 1, wantError: "refresh failed"},
		{name: "retry exhausted", states: []state{old, current, current, renamed}, existing: true, wantLookups: 2, wantError: "retry exhausted"},
		{name: "missing PR", states: []state{old}, missingNode: true, wantError: "pull request"},
		{name: "empty add", states: []state{empty}, wantLookups: 1},
		{name: "empty update", filename: "OWNERS", states: []state{empty, empty}, existing: true, wantReport: true, wantEmpty: true, wantLookups: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			git(root, "clone", "--depth=1", "--single-branch", "--branch=pr-head", "file://"+root, cwd)
			filename := tc.filename
			if filename == "" {
				filename = "CODENOTIFY"
			}
			eventPath := filepath.Join(t.TempDir(), "event.json")
			event := fmt.Sprintf(`{"number":123,"repository":{"name":"repo","owner":{"login":"test"}},"pull_request":{"node_id":"PR_test","head":{"sha":%q},"base":{"sha":%q},"user":{"login":"event-author"}}}`, head, oldBase)
			if err := ioutil.WriteFile(eventPath, []byte(event), 0600); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]string{
				"GITHUB_ACTIONS": "true", "GITHUB_EVENT_PATH": eventPath,
				"GITHUB_WORKSPACE": cwd, "GITHUB_GRAPHQL_URL": "https://github.test/graphql",
				"GITHUB_API_URL": "https://github.test/api/v3",
				"GITHUB_TOKEN":   "test-only-token", "INPUT_FILENAME": filename,
				"INPUT_SUBSCRIBER-THRESHOLD": "0",
			} {
				t.Setenv(name, value)
			}
			transport, originalVerbose := http.DefaultTransport, verbose
			t.Cleanup(func() { http.DefaultTransport, verbose = transport, originalVerbose })
			verbose = ioutil.Discard
			var calls []string
			var bodies []string
			stateCalls, lookups := 0, 0
			http.DefaultTransport = fakeGitHub(func(query string, variables map[string]string) (int, string) {
				switch {
				case query == "GET /api/v3/repos/test/repo/pulls/123":
					calls = append(calls, "state")
					stateCalls++
					if stateCalls > len(tc.states) {
						t.Fatal("more state refreshes than allowed")
					}
					if stateCalls == tc.errorAt {
						return http.StatusServiceUnavailable, `{"message":"refresh failed"}`
					}
					if tc.missingNode {
						return http.StatusOK, `null`
					}
					s := tc.states[stateCalls-1]
					data, err := json.Marshal(map[string]interface{}{
						"base":  map[string]string{"ref": s.BaseRef, "sha": s.BaseSHA},
						"head":  map[string]string{"sha": s.HeadSHA},
						"state": s.State, "draft": s.Draft,
						"user": map[string]string{"login": "author"}, "commits": 3,
					})
					if err != nil {
						t.Fatal(err)
					}
					return http.StatusOK, string(data)
				case strings.Contains(query, "query GetPullRequestComments"):
					calls = append(calls, "comments")
					lookups++
					if tc.existing {
						return http.StatusOK, fmt.Sprintf(`{"data":{"node":{"comments":{"nodes":[{"id":"comment_test","body":%q,"author":{"login":"bot"}}]}}}}`, "<!-- codenotify:"+filename+" report -->\nold report")
					}
					return http.StatusOK, `{"data":{"node":{"comments":{"nodes":[]}}}}`
				case strings.Contains(query, "mutation"):
					if len(calls) < 2 || calls[len(calls)-1] != "state" || calls[len(calls)-2] != "comments" {
						t.Error("comment mutation was not immediately preceded by a final state check after comment lookup")
					}
					calls = append(calls, "mutation")
					operation, idKey, id := "AddComment", "subjectId", "PR_test"
					if tc.existing {
						operation, idKey, id = "UpdateComment", "id", "comment_test"
					}
					if !strings.Contains(query, "mutation "+operation) || variables[idKey] != id {
						t.Errorf("wrong comment mutation: %s %v", query, variables)
					}
					bodies = append(bodies, variables["body"])
				default:
					t.Fatalf("unexpected GraphQL query: %s", query)
				}
				return http.StatusOK, `{"data":{}}`
			})

			err := testableMain(ioutil.Discard, nil)
			if tc.wantError == "" && err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Errorf("want error containing %q, got %v", tc.wantError, err)
			}
			if stateCalls != len(tc.states) || lookups != tc.wantLookups {
				t.Errorf("state queries=%d, calculations=%d; want %d, %d", stateCalls, lookups, len(tc.states), tc.wantLookups)
			}
			wantMutations := 0
			if tc.wantReport {
				wantMutations = 1
			}
			if len(bodies) != wantMutations {
				t.Fatalf("got %d mutations, want %d", len(bodies), wantMutations)
			}
			if tc.wantReport {
				base := newBase
				if tc.wantEmpty {
					base = head
				}
				for _, text := range []string{"<!-- codenotify:" + filename + " report -->", base + "..." + head} {
					if !strings.Contains(bodies[0], text) {
						t.Errorf("report lacks %q: %s", text, bodies[0])
					}
				}
				if tc.wantEmpty {
					if !strings.Contains(bodies[0], "No notifications.") {
						t.Errorf("expected empty update: %s", bodies[0])
					}
				} else if !strings.Contains(bodies[0], "| @intended | intended.txt |") {
					t.Errorf("missing intended notification: %s", bodies[0])
				}
				for _, text := range []string{oldBase + "...", "unrelated.txt", "@unrelated", "@old-subscriber", "@author"} {
					if strings.Contains(bodies[0], text) {
						t.Errorf("stale or excluded content %q: %s", text, bodies[0])
					}
				}
			}
		})
	}
}

func TestCliOptions(t *testing.T) {
	var originalVerbose io.Writer = verbose
	defer func() { verbose = originalVerbose }()
	tests := []struct {
		name    string
		args    []string
		verbose io.Writer
	}{
		{
			name:    "no arguments",
			args:    []string{},
			verbose: ioutil.Discard,
		},
		{
			name:    "verbose option",
			args:    []string{"-verbose"},
			verbose: os.Stderr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			cliOptions(stdout, test.args)
			if verbose != test.verbose {
				t.Errorf("expected verbose to be %v; got %v", test.verbose, verbose)
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
