// Package pproffd is for detecting resource leaks due to unclosed handles.
package pproffd

import (
	"io"
	"net"
	"os"
	"runtime/pprof"
)

const enabled = false

var p *pprof.Profile

func init() {
	if enabled {
		p = pprof.NewProfile("fds")
	}
}

type fd int

func (me *fd) Closed() {
	if enabled {
		p.Remove(me)
	}
}

func add(skip int) (ret *fd) {
	if enabled {
		ret = new(fd)
		p.Add(ret, skip+2)
	}
	return
}

type CloseWrapper struct {
	fd *fd
	c  io.Closer
}

func (me CloseWrapper) Close() error {
	me.fd.Closed()
	return me.c.Close()
}

func NewCloseWrapper(c io.Closer) CloseWrapper {
	// TODO: Check enabled?
	return CloseWrapper{
		fd: add(2),
		c:  c,
	}
}

type wrappedNetConn struct {
	net.Conn
	CloseWrapper
}

func (me wrappedNetConn) Close() error {
	return me.CloseWrapper.Close()
}

// Tracks a net.Conn until Close() is explicitly called.
func WrapNetConn(nc net.Conn) net.Conn {
	if !enabled {
		return nc
	}
	if nc == nil {
		return nil
	}
	return wrappedNetConn{
		nc,
		NewCloseWrapper(nc),
	}
}

type OSFile interface {
	io.Reader
	io.Seeker
	io.Closer
	io.Writer
	Stat() (os.FileInfo, error)
	io.ReaderAt
	io.WriterAt
}

type wrappedOSFile struct {
	*os.File
	CloseWrapper
}

func (me wrappedOSFile) Close() error {
	return me.CloseWrapper.Close()
}

func WrapOSFile(f *os.File) OSFile {
	if !enabled {
		return f
	}
	return &wrappedOSFile{f, NewCloseWrapper(f)}
}
