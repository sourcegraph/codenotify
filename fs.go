package codenotify

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
)

type FS interface {
	Open(name string) (File, error)
}

type File interface {
	Stat() (os.FileInfo, error)
	Read([]byte) (int, error)
	Close() error
}

// memfile is an in-memory file
type Memfile struct {
	*bytes.Buffer
}

func (m Memfile) Close() error {
	m.Buffer = nil
	return nil
}

func (m Memfile) Stat() (os.FileInfo, error) {
	return nil, errors.New("memfile does not support stat")
}

// Gitfs implements the FS interface for files at a specific git revision.
type Gitfs struct {
	cwd string
	rev string
}

func NewGitFS(cwd string, rev string) *Gitfs {
	return &Gitfs{cwd: cwd, rev: rev}
}

func (g *Gitfs) Open(name string) (File, error) {
	cmd := exec.Command("git", "-C", g.cwd, "show", g.rev+":"+name)
	buf, err := cmd.Output()
	if err != nil {
		return nil, os.ErrNotExist
	}
	return Memfile{
		Buffer: bytes.NewBuffer(buf),
	}, nil
}
