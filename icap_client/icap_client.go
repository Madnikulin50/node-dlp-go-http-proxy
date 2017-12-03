package icap_client

import (
	// "errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/elazarl/goproxy/transport"
	"path/filepath"
	"strings"
)

type IcapClientSetting struct {
	address string
	reqmode bool
	respmode bool
	preview int
}

var icapSettings = IcapClientSetting{"127.0.0.1:1344", true, true, -1}


type IcapConnection struct {
	conn     *net.Conn
}

var icapSessionsMap = make(map[int64]*IcapConnection)

type Meta struct {
	req      *http.Request
	resp     *http.Response
	err      error
	t        time.Time
	sess     int64
	bodyPath string
	from     string
	conn     *IcapConnection
}

type FileStream struct {
	path string
	f    *os.File
	meta *Meta
}

func NewFileStream(path string, meta *Meta) *FileStream {
	return &FileStream{path, nil, meta}
}

func (fs *FileStream) Write(b []byte) (nr int, err error) {

	if fs.f == nil {
		fs.f, err = os.Create(fs.path)
		if err != nil {
			return 0, err
		}
	}

	/* if fs.meta.conn == nil {
		conn, err := net.Dial("tcp", icapSettings.address)
		if err != nil {
			return 0, err
		}
		fs.meta.conn = &conn

	} */
	return fs.f.Write(b)
}

func (fs *FileStream) Close() error {
	fmt.Println("Close", fs.path)
	if fs.f == nil {
		return nil
	}
	return fs.f.Close()
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

func (m *Meta) WriteTo(w io.Writer) (nr int64, err error) {
	if m.req != nil {
		fprintf(&nr, &err, w, "Type: request\r\n")
	} else if m.resp != nil {
		fprintf(&nr, &err, w, "Type: response\r\n")
	}
	fprintf(&nr, &err, w, "ReceivedAt: %v\r\n", m.t)
	fprintf(&nr, &err, w, "Session: %d\r\n", m.sess)
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

func (m *Meta) SendIcap() (err error) {
	if m.conn.conn == nil {
		conn, err := net.Dial("tcp", icapSettings.address)
		if err != nil {
			return err
		}
		m.conn.conn = &conn
	}
	if m.resp == nil {
		buf, err2 := httputil.DumpRequest(m.req, false)
		if err2 != nil {
			return err2
		}
		fmt.Fprintf(*m.conn.conn, "REQMOD icap://%v/reqmode ICAP/1.0\r\nHost: %v\r\nEncapsulated: req-hdr=0, null-body=%d\r\n\r\n%s",
			icapSettings.address, icapSettings.address, len(buf), buf)
	} else if m.resp != nil && icapSettings.respmode {
		bufReq, err2 := httputil.DumpRequest(m.req, false)
		if err2 != nil {
			return err2
		}
		bufResp, err2 := httputil.DumpResponse(m.resp, false)
		if err2 != nil {
			return err2
		}
		fmt.Fprintf(*m.conn.conn, "RESPMOD icap://%v/respmode\r\nHost: %v\r\nEncapsulated: req-hdr=0, resp-hdr=%d, null-body=%d\r\n\r\n%s%s",
			icapSettings.address, icapSettings.address, len(bufReq), len(bufReq) + len(bufResp), bufReq, bufResp)
	}

	return
}

// IcapClient is an asynchronous HTTP request/response logger. It traces
// requests and responses headers in a "log" file in logger directory and dumps
// their bodies in files prefixed with the session identifiers.
// Close it to ensure pending items are correctly logged.
type IcapClient struct {
	path  string
	c     chan *Meta
	errch chan error
}

func RemoveContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func NewIcapClient(basepath string) (*IcapClient, error) {
	err := RemoveContents(basepath)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(path.Join(basepath, "log.txt"))
	if err != nil {
		return nil, err
	}
	logger := &IcapClient{basepath, make(chan *Meta), make(chan error)}
	go func() {
		for m := range logger.c {
			if _, err := m.WriteTo(f); err != nil {
				log.Println("Can't write meta", err)
			}
			if err := m.SendIcap(); err != nil {
				log.Println("Can't write icap", err)
			}
		}
		logger.errch <- f.Close()
	}()
	return logger, nil
}

func getExtensionFromContentType(ct string) string {
	i := strings.IndexRune(ct, '/')
	if i == -1 {
		return "undefined"
	}
	result := ct[i + 1:]
	i = strings.IndexRune(result, ';')
	if i != -1 {
		result = result[:i]
	}
	i = strings.IndexRune(result, '+')
	if i != -1 {
		result = result[:i]
	}
	if result == "plain" {
		return "txt"
	}
	if result == "x-javascript" {
		return "js"
	}
	if result == "javascript" {
		return "js"
	}
	if result == "x-font-woff" {
		return "woff"
	}
	if result == "x-icon" {
		return "ico"
	}

	return result
}

func (logger *IcapClient) LogResp(resp *http.Response, ctx *goproxy.ProxyCtx) {
	from := ""
	if ctx.UserData != nil {
		from = ctx.UserData.(*transport.RoundTripDetails).TCPAddr.String()
	}
	inResp := resp
	if inResp == nil {
		inResp = emptyResp
	}
	meta := Meta{
		req: ctx.Req,
		resp: inResp,
		err:  ctx.Error,
		t:    time.Now(),
		sess: ctx.Session,
		from: from,
		conn: &IcapConnection{}}

	if resp != nil {
		if resp.ContentLength > 0 ||
					resp.ContentLength == -1 {
			contentType := resp.Header.Get("Content-Type")
			body := path.Join(logger.path, fmt.Sprintf("%d_resp.%s", ctx.Session, getExtensionFromContentType(contentType)))
			resp.Body = NewTeeReadCloser(resp.Body, NewFileStream(body, &meta))
		}

	}
	logger.LogMeta(&meta)
}

var emptyResp = &http.Response{}
var emptyReq = &http.Request{}

func (logger *IcapClient) LogReq(req *http.Request, ctx *goproxy.ProxyCtx) {
	inReq := req
	if inReq == nil {
		inReq = emptyReq
	}
	meta := Meta{
		req:  inReq,
		err:  ctx.Error,
		t:    time.Now(),
		sess: ctx.Session,
		from: req.RemoteAddr,
		conn: &IcapConnection{}}
	if req != nil {
		if req.ContentLength > 0 || req.ContentLength == -1 {
			contentType := req.Header.Get("Content-Type")
			body := path.Join(logger.path, fmt.Sprintf("%d_req.%s", ctx.Session, getExtensionFromContentType(contentType)))
			req.Body = NewTeeReadCloser(req.Body, NewFileStream(body, &meta))
		}
	}
	logger.LogMeta(&meta)
}

func (logger *IcapClient) LogMeta(m *Meta) {
	logger.c <- m
}

func (logger *IcapClient) Close() error {
	close(logger.c)
	return <-logger.errch
}

// TeeReadCloser extends io.TeeReader by allowing reader and writer to be
// closed.
type TeeReadCloser struct {
	r io.Reader
	w io.WriteCloser
	c io.Closer
}

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
