package main

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"testing"
)

// memfs is an in-memory implementation of the FS interface.
type memfs map[string]string

func (m memfs) paths() []string {
	paths := []string{}
	for path := range m {
		paths = append(paths, path)
	}
	return paths
}

type memfile struct {
	*bytes.Buffer
}

func (m memfile) Close() error {
	m.Buffer = nil
	return nil
}

func (m memfile) Stat() (os.FileInfo, error) {
	return nil, nil
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

func TestNotifications(t *testing.T) {
	tests := []struct {
		name          string
		fs            memfs
		notifications map[string][]string
		err           error
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
			if err != nil &&
				(test.err == nil || test.err.Error() != err.Error()) {
				t.Errorf("expected error %s; got %s", test.err, err)
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
