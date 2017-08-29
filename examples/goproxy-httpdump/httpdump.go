package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"path"
	"sync"
	"time"

	"github.com/toebes/goproxy2"
	"github.com/toebes/goproxy2/transport"
)

// FileStream tracks an output file by the name of the file and the open file handle
type FileStream struct {
	path string
	f    *os.File
}

// NewFileStream creates a new FileStream for a given path but with the file not yet opened
func NewFileStream(path string) *FileStream {
	return &FileStream{path, nil}
}

// Write writes a set of bytes to a FileStream
func (fs *FileStream) Write(b []byte) (nr int, err error) {
	if fs.f == nil {
		fs.f, err = os.Create(fs.path)
		if err != nil {
			return 0, err
		}
	}
	return fs.f.Write(b)
}

// Close closes a FileStream
func (fs *FileStream) Close() error {
	fmt.Println("Close", fs.path)
	if fs.f == nil {
		return errors.New("FileStream was never written into")
	}
	return fs.f.Close()
}

// Meta holds all the information about a Request/Response
type Meta struct {
	req      *http.Request
	resp     *http.Response
	err      error
	t        time.Time
	session  int64
	bodyPath string
	from     string
}

func fprintf(nr *int64, err *error, w io.Writer, pat string, a ...interface{}) {
	if *err != nil {
		return
	}
	var n int
	n, *err = fmt.Fprintf(w, pat, a...)
	*nr += int64(n)
}

func write(nr *int64, err *error, w io.Writer, b []byte) {
	if *err != nil {
		return
	}
	var n int
	n, *err = w.Write(b)
	*nr += int64(n)
}

// WriteTo dumps the data about an open stream to a file
func (m *Meta) WriteTo(w io.Writer) (nr int64, err error) {
	if m.req != nil {
		fprintf(&nr, &err, w, "Type: request\r\n")
	} else if m.resp != nil {
		fprintf(&nr, &err, w, "Type: response\r\n")
	}
	fprintf(&nr, &err, w, "ReceivedAt: %v\r\n", m.t)
	fprintf(&nr, &err, w, "Session: %d\r\n", m.session)
	fprintf(&nr, &err, w, "From: %v\r\n", m.from)
	if m.err != nil {
		// note the empty response
		fprintf(&nr, &err, w, "Error: %v\r\n\r\n\r\n\r\n", m.err)
	} else if m.req != nil {
		fprintf(&nr, &err, w, "\r\n")
		buf, err2 := httputil.DumpRequest(m.req, false)
		if err2 != nil {
			return nr, err2
		}
		write(&nr, &err, w, buf)
	} else if m.resp != nil {
		fprintf(&nr, &err, w, "\r\n")
		buf, err2 := httputil.DumpResponse(m.resp, false)
		if err2 != nil {
			return nr, err2
		}
		write(&nr, &err, w, buf)
	}
	return
}

// HTTPLogger is an asynchronous HTTP request/response logger. It traces
// requests and responses headers in a "log" file in logger directory and dumps
// their bodies in files prefixed with the session identifiers.
// Close it to ensure pending items are correctly logged.
type HTTPLogger struct {
	path    string
	c       chan *Meta
	errChan chan error
}

// NewLogger creates a new logger for a given path
func NewLogger(basepath string) (*HTTPLogger, error) {
	// Create the log that we will be appending to
	f, err := os.Create(path.Join(basepath, "log"))
	if err != nil {
		return nil, err
	}
	// Spawn the logger process to consume and output the data
	logger := &HTTPLogger{basepath, make(chan *Meta), make(chan error)}
	go func() {
		for m := range logger.c {
			if _, err := m.WriteTo(f); err != nil {
				log.Println("Can't write meta", err)
			}
		}
		logger.errChan <- f.Close()
	}()
	return logger, nil
}

// LogResp dumps out the response from a logged session
func (logger *HTTPLogger) LogResp(req *http.Request, resp *http.Response) {
	ctx := goproxy.GetAnyProxyCtx(req)
	body := path.Join(logger.path, fmt.Sprintf("%d_resp", ctx.Session))
	from := ""
	if ctx.UserData != nil {
		from = ctx.UserData.(*transport.RoundTripDetails).TCPAddr.String()
	}
	if resp == nil {
		resp = emptyResp
	} else {
		resp.Body = NewTeeReadCloser(resp.Body, NewFileStream(body))
	}
	logger.LogMeta(&Meta{
		resp:    resp,
		err:     ctx.Error,
		t:       time.Now(),
		session: ctx.Session,
		from:    from})
}

var emptyResp = &http.Response{}
var emptyReq = &http.Request{}

// LogReq dumps out the request for a logged session
func (logger *HTTPLogger) LogReq(req *http.Request) {
	ctx := goproxy.GetAnyProxyCtx(req)
	body := path.Join(logger.path, fmt.Sprintf("%d_req", ctx.Session))
	if req == nil {
		req = emptyReq
	} else {
		req.Body = NewTeeReadCloser(req.Body, NewFileStream(body))
	}
	logger.LogMeta(&Meta{
		req:     req,
		err:     ctx.Error,
		t:       time.Now(),
		session: ctx.Session,
		from:    req.RemoteAddr})
}

// LogMeta associates the meta data with a given Logger channel
func (logger *HTTPLogger) LogMeta(m *Meta) {
	logger.c <- m
}

// Close closes down a logger channel
func (logger *HTTPLogger) Close() error {
	close(logger.c)
	return <-logger.errChan
}

// TeeReadCloser extends io.TeeReader by allowing reader and writer to be
// closed.
type TeeReadCloser struct {
	r io.Reader
	w io.WriteCloser
	c io.Closer
}

// NewTeeReadCloser creates a TeeReadCloser
func NewTeeReadCloser(r io.ReadCloser, w io.WriteCloser) io.ReadCloser {
	return &TeeReadCloser{io.TeeReader(r, w), w, r}
}

func (t *TeeReadCloser) Read(b []byte) (int, error) {
	return t.r.Read(b)
}

// Close attempts to close the reader and write. It returns an error if both
// failed to Close.
func (t *TeeReadCloser) Close() error {
	err1 := t.c.Close()
	err2 := t.w.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// stoppableListener serves stoppableConn and tracks their lifetime to notify
// when it is safe to terminate the application.
type stoppableListener struct {
	net.Listener
	sync.WaitGroup
}

type stoppableConn struct {
	net.Conn
	wg *sync.WaitGroup
}

func newStoppableListener(l net.Listener) *stoppableListener {
	return &stoppableListener{l, sync.WaitGroup{}}
}

func (sl *stoppableListener) Accept() (net.Conn, error) {
	c, err := sl.Listener.Accept()
	if err != nil {
		return c, err
	}
	sl.Add(1)
	return &stoppableConn{c, &sl.WaitGroup}, nil
}

func (sc *stoppableConn) Close() error {
	sc.wg.Done()
	return sc.Conn.Close()
}

func main() {
	verbose := flag.Bool("v", true, "should every proxy request be logged to stdout")
	addr := flag.String("l", ":8080", "on which address should the proxy listen")
	flag.Parse()
	proxy := goproxy.New()
	proxy.Verbose(*verbose)

	// Create a directory to hold all the logger files
	if err := os.MkdirAll("db", 0755); err != nil {
		log.Fatal("Can't create dir", err)
	}
	// Make sure we can utilize the maximum number of processes for multi-threading
	//runtime.GOMAXPROCS(runtime.NumCPU())

	// Start up the logger process
	logger, err := NewLogger("db")
	if err != nil {
		log.Fatal("can't open log file", err)
	}
	//tr := transport.Transport{Proxy: transport.ProxyFromEnvironment}
	// For every incoming request, override the RoundTripper to extract
	// connection information. Store it in a session context so we can log it after
	// handling the response.
	proxy.OnRequest().DoFunc(func(req *http.Request) (*http.Request, *http.Response) {
		//	ctx := goproxy.GetAnyProxyCtx(req)
		//	ctx.RoundTripper = &tr
		logger.LogReq(req)
		return req, nil
	})
	proxy.OnResponse().DoFunc(func(req *http.Request, resp *http.Response) (*http.Request, *http.Response) {
		logger.LogResp(req, resp)
		return nil, resp
	})
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal("listen:", err)
	}
	sl := newStoppableListener(l)
	ch := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		log.Println("Got SIGINT exiting")
		sl.Add(1)
		sl.Close()
		logger.Close()
		sl.Done()
	}()
	log.Println("Starting Proxy")
	http.Serve(sl, proxy)
	sl.Wait()
	log.Println("All connections closed - exit")
}
