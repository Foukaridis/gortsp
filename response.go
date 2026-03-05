package rtsp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A server response looks like this:
// RTSP/1.0 200 OK\r\n
// CSeq: 1\r\n
// Public: OPTIONS, DESCRIBE, SETUP, PLAY\r\n
// \r\n
// Or with a body (what DESCRIBE returns):
// RTSP/1.0 200 OK\r\n
// CSeq: 2\r\n
// Content-Type: application/sdp\r\n
// Content-Length: 134\r\n
// \r\n
// v=0\r\n
// o=- 0 0 IN IP4 127.0.0.1\r\n
// ... (SDP body)
// So a Response needs:

// Status code (200, 401, 404...)
// Status message ("OK", "Unauthorized"...)
// Headers map
// Optional body (string)

type Response struct {
	StatusCode int
	StatusMsg  string
	Headers    map[string]string
	Body       string
}

func ReadResponse(r io.Reader) (*Response, error) {
	// Step 1 — status line:

	// Read one line with reader.ReadString('\n')
	// It looks like RTSP/1.0 200 OK\r\n
	// Use strings.SplitN(line, " ", 3) to get 3 parts
	// Parts are: [0] protocol (ignore), [1] status code, [2] status message
	// Convert [1] to int with strconv.Atoi
	// Trim \r\n off [2]
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid response line: %s", line)
	}
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %s", parts[1])
	}

	statusMessage := strings.TrimSpace(parts[2])
	// Step 2 — headers:

	// Loop: read a line, if it's just \r\n or empty — stop
	// Each header line looks like Key: Value\r\n
	// Split on ": " with strings.SplitN(line, ": ", 2)
	// Store trimmed key/value in your map
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // End of headers
		}
		headerParts := strings.SplitN(line, ": ", 2)
		if len(headerParts) == 2 {
			headers[headerParts[0]] = headerParts[1]
		}
	}
	// Step 3 — body:

	// Check if headers["Content-Length"] exists
	// If it does, convert to int, then read exactly that many bytes:
	body := ""
	if contentLengthStr, ok := headers["Content-Length"]; ok {
		contentLength, err := strconv.Atoi(contentLengthStr)
		if err != nil {
			return nil, fmt.Errorf("invalid Content-Length: %s", contentLengthStr)
		}
		bodyBytes := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, bodyBytes); err != nil {
			return nil, err
		}
		body = string(bodyBytes)
	}
	return &Response{
		StatusCode: statusCode,
		StatusMsg:  statusMessage,
		Headers:    headers,
		Body:       body,
	}, nil
}
