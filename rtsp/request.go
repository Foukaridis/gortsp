package rtsp

import (
	"fmt"
	"io"
)

// An RTSP request on the wire looks like this:
// OPTIONS rtsp://admin:secret@localhost:8554/cam1 RTSP/1.0\r\n
// CSeq: 1\r\n
// User-Agent: my-rtsp-lib\r\n
// \r\n
// So a Request needs:

// A method (OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN)
// A URL
// A sequence number (CSeq)
// Optional headers (for auth, transport, etc.)

type Request struct {
	Method  string
	URL     string
	CSeq    int
	Headers map[string]string
}

func (r *Request) Write(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s %s RTSP/1.0\r\n", r.Method, r.URL); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "CSeq: %d\r\n", r.CSeq); err != nil {
		return err
	}
	for k, v := range r.Headers {
		if _, err := fmt.Fprintf(w, "%s: %s\r\n", k, v); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, "\r\n")
	return err
}
