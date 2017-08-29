package regretable

import (
	"io"
)

// A Reader will allow you to read from a reader, and then
// to "regret" reading it, and push back everything you've read.
// For example,
//	rb := NewReader(bytes.NewBuffer([]byte{1,2,3}))
//	var b = make([]byte,1)
//	rb.Read(b) // b[0] = 1
//	rb.Regret()
//	ioutil.ReadAll(rb.Read) // returns []byte{1,2,3},nil
type Reader struct {
	reader   io.Reader
	overflow bool
	r, w     int
	buf      []byte
}

var defaultBufferSize = 500

// ReaderCloser is the same as Reader, but allows closing the underlying reader
type ReaderCloser struct {
	Reader
	c io.Closer
}

// Close closes the underlying readCloser, you cannot regret after closing the stream
func (rbc *ReaderCloser) Close() error {
	return rbc.c.Close()
}

// NewReaderCloser initializes a ReaderCloser with underlying readCloser rc
func NewReaderCloser(rc io.ReadCloser) *ReaderCloser {
	return &ReaderCloser{*NewReader(rc), rc}
}

// NewReaderCloserSize initializes a ReaderCloser with underlying readCloser rc
func NewReaderCloserSize(rc io.ReadCloser, size int) *ReaderCloser {
	return &ReaderCloser{*NewReaderSize(rc, size), rc}
}

// Regret causes the next read from the Reader to be as if the underlying reader
// was never read (or from the last point forget is called).
func (rb *Reader) Regret() {
	if rb.overflow {
		panic("regretting after overflow makes no sense")
	}
	rb.r = 0
}

// Forget will "forget" everything read so far.
//	rb := NewReader(bytes.NewBuffer([]byte{1,2,3}))
//	var b = make([]byte,1)
//	rb.Read(b) // b[0] = 1
//	rb.Forget()
//	rb.Read(b) // b[0] = 2
//	rb.Regret()
//	ioutil.ReadAll(rb.Read) // returns []byte{2,3},nil
func (rb *Reader) Forget() {
	if rb.overflow {
		panic("forgetting after overflow makes no sense")
	}
	rb.r = 0
	rb.w = 0
}

// NewReaderSize initializes a Reader with underlying reader r, whose buffer is size bytes long
func NewReaderSize(r io.Reader, size int) *Reader {
	return &Reader{reader: r, buf: make([]byte, size)}
}

// NewReader initializes a Reader with underlying reader r
func NewReader(r io.Reader) *Reader {
	return NewReaderSize(r, defaultBufferSize)
}

// reads from the underlying reader. Will buffer all input until Regret is called.
func (rb *Reader) Read(p []byte) (n int, err error) {
	if rb.overflow {
		return rb.reader.Read(p)
	}
	if rb.r < rb.w {
		n = copy(p, rb.buf[rb.r:rb.w])
		rb.r += n
		return
	}
	n, err = rb.reader.Read(p)
	bn := copy(rb.buf[rb.w:], p[:n])
	rb.w, rb.r = rb.w+bn, rb.w+n
	if bn < n {
		rb.overflow = true
	}
	return
}
