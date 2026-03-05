package rtsp

import (
	"bufio"
	"context"
	"net"
	"time"
)

type Conn struct {
	conn    net.Conn      // raw TCP connection
	reader  *bufio.Reader // buffered reader over the connection
	cseq    int           // auto-incremented per request
	timeout time.Duration
}

// tasks
// 1. Dial — opens the TCP connection:
// 2. Close:
// 3. Send — the key method:

func Dial(ctx context.Context, add string, timeout time.Duration) (*Conn, error) {
	// use net.DialTimeout or a net.Dialer with the context
	// wrap conn in a bufio.Reader
	// return &Conn{}
	netDialer := &net.Dialer{}
	conn, err := netDialer.DialContext(ctx, "tcp", add)
	if err != nil {
		return nil, err
	}
	return &Conn{
		conn:    conn,
		reader:  bufio.NewReader(conn),
		timeout: timeout,
	}, nil
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) Send(method, url string, headers map[string]string) (*Response, error) {
	// increment c.cseq
	c.cseq++
	// build a Request{} with the current cseq
	req := &Request{
		Method:  method,
		URL:     url,
		Headers: headers,
		CSeq:    c.cseq,
	}
	// set a deadline on c.conn using c.timeout
	if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, err
	}
	// call req.Write(c.conn)
	if err := req.Write(c.conn); err != nil {
		return nil, err
	}
	// call ReadResponse(c.reader)  ← note: pass c.reader not c.conn
	return ReadResponse(c.reader)
}

func (c *Conn) ReadRTPPacket() (*RTPPacket, error) {
	return ReadRTPPacket(c.reader)
}
