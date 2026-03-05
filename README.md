# rtsp

A minimal, zero-dependency RTSP client library written in Go for determining the health status of IP cameras.

## Overview

This library implements just enough of the RTSP/1.0 protocol (RFC 2326) to perform a full health check sequence against a camera stream. It does not aim to be a general-purpose RTSP client — it is purpose-built for liveness and health checking.

A camera is considered **healthy** when:
- A TCP connection can be established
- The RTSP handshake completes successfully (with auth if required)
- An SDP description is returned via `DESCRIBE`
- A valid RTP packet with pixel data is received after `PLAY`

## Package Structure

```
rtsp/
├── request.go    — Request struct, serialises to RTSP wire format
├── response.go   — Response struct, parses from RTSP wire format
├── client.go     — Conn, Dial, Send, ReadRTPPacket
├── auth.go       — Basic and Digest authentication
├── rtp.go        — Interleaved RTP frame reader and validator
└── health.go     — HealthCheck() — public entry point
```

## Usage

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/Foukaridis/gortsp"
)

func main() {
    ctx := context.Background()

    result := rtsp.HealthCheck(ctx, 1, "rtsp://admin:secret@192.168.1.10:554/stream", 5*time.Second)

    fmt.Printf("Camera %d: %s (%s)\n", result.CameraID, result.Status, result.Latency)
    if result.Error != "" {
        fmt.Printf("  Error: %s\n", result.Error)
    }
}
```

## Health Status Values

| Status | Meaning |
|---|---|
| `HEALTHY` | Full sequence completed — RTP data received |
| `UNAUTHENTICATED` | Camera reachable but credentials are invalid |
| `UNHEALTHY` | Connected and authenticated but stream unavailable |
| `OFFLINE` | TCP connection failed or no RTSP response |

## RTSP Sequence

The full sequence performed by `HealthCheck`:

```
1. TCP Dial          → OFFLINE if unreachable
2. OPTIONS           → OFFLINE if no response
3. (401?) Auth retry → UNAUTHENTICATED if credentials rejected
4. DESCRIBE          → UNHEALTHY if no SDP body returned
5. SETUP             → UNHEALTHY if session not established
6. PLAY              → UNHEALTHY if server rejects
7. Read RTP packet   → UNHEALTHY if no valid data received
8. TEARDOWN          → always sent, result ignored
```

## Authentication

Both **Basic** and **Digest** auth schemes are supported. Credentials are parsed directly from the RTSP URL:

```
rtsp://username:password@host:port/path
```

On a `401 Unauthorized` response, the library parses the `WWW-Authenticate` header, builds the appropriate `Authorization` header, and retries automatically. If the retry also returns `401`, the result is `UNAUTHENTICATED`.

Digest auth uses the standard MD5 challenge-response:

```
HA1      = MD5(username:realm:password)
HA2      = MD5(method:uri)
response = MD5(HA1:nonce:HA2)
```

> Note: MD5 is used here strictly for RFC 2617 Digest auth compatibility, not for security.

## Non-blocking Design

Every operation is deadline-bound. The `timeout` parameter passed to `HealthCheck` is applied as a per-operation deadline on the underlying TCP connection via `SetDeadline`. A hung or slow camera will never block longer than the specified timeout.

For checking multiple cameras concurrently, fan out with goroutines:

```go
results := make(chan rtsp.HealthResult, len(cameras))
var wg sync.WaitGroup

for _, cam := range cameras {
    wg.Add(1)
    go func(cam Camera) {
        defer wg.Done()
        results <- rtsp.HealthCheck(ctx, cam.ID, cam.RTSPURL, 5*time.Second)
    }(cam)
}

go func() {
    wg.Wait()
    close(results)
}()

for result := range results {
    fmt.Printf("Camera %d: %s\n", result.CameraID, result.Status)
}
```

## HealthResult

```go
type HealthResult struct {
    CameraID int
    Status   HealthStatus
    Latency  time.Duration
    Error    string        // empty if healthy
}
```

## No External Dependencies

The library uses only the Go standard library:

| Package | Used for |
|---|---|
| `net` | Raw TCP connection |
| `bufio` | Buffered reading from the connection |
| `crypto/md5` | Digest auth response hash |
| `encoding/base64` | Basic auth encoding |
| `encoding/binary` | RTP interleaved frame length (big-endian) |
| `net/url` | Parsing credentials from RTSP URL |

## Design Decisions

**Why not use an existing RTSP library?**
Building the protocol layer directly gives full control over connection lifecycle, deadline handling, and exactly how much of the protocol is exercised. For health checking, most RTSP libraries do too much — they buffer media, manage tracks, and handle reconnection. This library does the minimum required to prove a stream is alive.

**Why only read one RTP packet?**
One valid RTP packet proves the full path: network reachable → authenticated → stream exists → media is flowing. Reading more would just add latency with no additional signal.

**Why no RTSP over UDP?**
UDP interleaving is not implemented. TCP interleaved mode (`RTP/AVP/TCP`) is used exclusively, which is standard for health checking behind NAT and firewalls, and is supported by all modern IP cameras.

**Why is `ReadRTPPacket` also a method on `Conn`?**
The underlying `bufio.Reader` is unexported to prevent callers from consuming bytes off the wire out of order. `conn.ReadRTPPacket()` delegates to the standalone `ReadRTPPacket(r io.Reader)` function, which keeps the function independently testable while keeping the buffer encapsulated.