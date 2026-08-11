package completion_api

import (
	"bufio"
	"net"
	"net/http"
)

// delayedWriter withholds headers/status until Commit is called.
// Used so account fallback can still run before the client sees a response.
type delayedWriter struct {
	http.ResponseWriter
	code      int
	hdr       http.Header
	committed bool
	buf       []byte
}

func newDelayedWriter(w http.ResponseWriter) *delayedWriter {
	return &delayedWriter{
		ResponseWriter: w,
		code:           http.StatusOK,
		hdr:            make(http.Header),
	}
}

func (d *delayedWriter) Header() http.Header {
	if d.committed {
		return d.ResponseWriter.Header()
	}
	return d.hdr
}

func (d *delayedWriter) WriteHeader(statusCode int) {
	if d.committed {
		return
	}
	d.code = statusCode
}

func (d *delayedWriter) Write(p []byte) (int, error) {
	if !d.committed {
		d.buf = append(d.buf, p...)
		return len(p), nil
	}
	return d.ResponseWriter.Write(p)
}

func (d *delayedWriter) Commit() error {
	if d.committed {
		return nil
	}
	dst := d.ResponseWriter.Header()
	for k, vv := range d.hdr {
		dst[k] = append([]string(nil), vv...)
	}
	d.ResponseWriter.WriteHeader(d.code)
	d.committed = true
	if len(d.buf) > 0 {
		_, err := d.ResponseWriter.Write(d.buf)
		d.buf = nil
		return err
	}
	return nil
}

func (d *delayedWriter) Flush() {
	if !d.committed {
		_ = d.Commit()
	}
	if f, ok := d.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (d *delayedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := d.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
