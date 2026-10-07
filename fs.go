package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
)

type FS interface {
	Open(name string) (File, error)
	Paths(name string) ([]string, error)
}

type File interface {
	Stat() (os.FileInfo, error)
	Read([]byte) (int, error)
	Close() error
}

// memfile is an in-memory file
type memfile struct {
	*bytes.Buffer
}

func (m memfile) Close() error {
	m.Buffer = nil
	return nil
}

func (m memfile) Stat() (os.FileInfo, error) {
	return nil, errors.New("memfile does not support stat")
}

// gitfs implements the FS interface for files at a specific git revision.
type gitfs struct {
	cwd string
	rev string
}

func (g *gitfs) Open(name string) (File, error) {
	cmd := exec.Command("git", "-C", g.cwd, "show", g.rev+":"+name)
	buf, err := cmd.Output()
	if err != nil {
		return nil, os.ErrNotExist
	}
	return memfile{
		Buffer: bytes.NewBuffer(buf),
	}, nil
}

// Paths returns the paths of every file with the given name at the revision
func (g *gitfs) Paths(name string) ([]string, error) {
	cmd := exec.Command("git", "-C", g.cwd, "ls-tree", "-r", "-z", "--name-only", g.rev)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error listing files in %s: %w", g.rev, err)
	}

	paths := []string{}
	for _, file := range strings.Split(string(out), "\x00") {
		if file != "" && path.Base(file) == name {
			paths = append(paths, file)
		}
	}
	return paths, nil
}
