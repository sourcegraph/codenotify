package main

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"testing"
)

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
			name: "wildcard flat",
			fs: memfs{
				"CODENOTIFY": "* @allnotify\n",
				"file.js":    "",
				"file.go":    "",
			},
			notifications: map[string][]string{
				"@allnotify": {"CODENOTIFY", "file.js", "file.go"},
			},
		},
		{
			name: "multiple subscribers",
			fs: memfs{
				"CODENOTIFY": "* @alice @bob\n",
				"file.js":    "",
				"file.go":    "",
			},
			notifications: map[string][]string{
				"@alice": {"CODENOTIFY", "file.js", "file.go"},
				"@bob":   {"CODENOTIFY", "file.js", "file.go"},
			},
		},
		// {
		// 	name: "all",
		// 	files: map[string]string{
		// 		"CODENOTIFY": "\n" +
		// 			"* @allnotify\n" +
		// 			"*.go @gonotify\n" +
		// 			"*.js @jsnotify\n",
		// 		"file.md": "",
		// 		"file.js": "",
		// 		"file.go": "",
		// 		"dir/CODENOTIFY": "\n" +
		// 			"* @dirnotify\n" +
		// 			"*.go @dirgonotify\n" +
		// 			"*.js @dirjsnotify\n",
		// 		"dir/file.md": "",
		// 		"dir/file.go": "",
		// 		"dir/file.js": "",
		// 	},
		// 	notifications: map[string][]string{
		// 		"@allnotify":   {"file.md", "file.js", "file.go", "dir/file.md", "dir/file.go", "dir/file.js"},
		// 		"@gonotify":    {"file.go", "dir/file.go"},
		// 		"@jsnotify":    {"file.js", "dir/file.js"},
		// 		"@dirnotify":   {"dir/file.md", "dir/file.go", "dir/file.js"},
		// 		"@dirgonotify": {"dir/file.go"},
		// 		"@dirjsnotify": {"dir/file.js"},
		// 	},
		// },
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifs, err := notifications(test.fs, test.fs.paths())
			if err != nil &&
				(test.err == nil || test.err.Error() != err.Error()) {
				t.Errorf("expected error %s; got %s", test.err, err)
			}

			for subscriber, actualfiles := range notifs {
				expectedfiles := test.notifications[subscriber]
				sort.Strings(expectedfiles)
				sort.Strings(actualfiles)
				if !reflect.DeepEqual(expectedfiles, actualfiles) {
					t.Errorf("%s expected notifications for %v; got %v", subscriber, expectedfiles, actualfiles)
				}
			}

			for subscriber, expectedfiles := range test.notifications {
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
