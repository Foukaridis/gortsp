package rtsp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type HealthStatus string

const (
	StatusHealthy         HealthStatus = "HEALTHY"
	StatusUnauthenticated HealthStatus = "UNAUTHENTICATED"
	StatusUnhealthy       HealthStatus = "UNHEALTHY"
	StatusOffline         HealthStatus = "OFFLINE"
)

type HealthResult struct {
	CameraID int
	Status   HealthStatus
	Latency  time.Duration
	Error    string // human readable, empty if healthy
}

func offlineResult(id int, start time.Time, err error) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   StatusOffline,
		Latency:  time.Since(start),
		Error:    err.Error(),
	}
}

func unhealthyResult(id int, start time.Time, err error) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   StatusUnhealthy,
		Latency:  time.Since(start),
		Error:    err.Error(),
	}
}

func unauthenticatedResult(id int, start time.Time) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   StatusUnauthenticated,
		Latency:  time.Since(start),
		Error:    "invalid credentials",
	}
}

func HealthCheck(ctx context.Context, cameraID int, rtspURL string, timeout time.Duration) HealthResult {
	start := time.Now()
	var authHeaderValue string
	// 1. parse credentials out of the URL
	//    hint: use net/url — url.Parse(rtspURL) gives you u.User.Username() and u.User.Password()
	u, err := url.Parse(rtspURL)
	if err != nil {
		return offlineResult(cameraID, start, fmt.Errorf("invalid URL: %v", err))
	}
	username := u.User.Username()
	password, _ := u.User.Password()

	// 2. Dial — if err → return OFFLINE
	conn, err := Dial(ctx, u.Host, timeout)
	if err != nil {
		return offlineResult(cameraID, start, fmt.Errorf("failed to connect: %v", err))
	}

	// 3. defer conn.Close()
	defer conn.Close()

	// 4. Send OPTIONS
	//    if err → return OFFLINE
	//    if status 401 → go to step 5
	//    if status != 200 → return OFFLINE
	resp, err := conn.Send("OPTIONS", rtspURL, nil)
	if err != nil {
		return offlineResult(cameraID, start, fmt.Errorf("OPTIONS failed: %v", err))
	}
	if resp.StatusCode == 401 {
		// 5. (auth) parse WWW-Authenticate header
		//    call BuildAuthorisation
		//    retry OPTIONS with Authorization header
		//    if still 401 → return UNAUTHENTICATED
		//    if err → return OFFLINE
		authHeader, ok := resp.Headers["WWW-Authenticate"]
		if !ok {
			return unhealthyResult(cameraID, start, fmt.Errorf("missing WWW-Authenticate header"))
		}
		authChallenge, err := ParseAuthChallenge(authHeader)
		if err != nil {
			return unhealthyResult(cameraID, start, fmt.Errorf("failed to parse auth challenge: %v", err))
		}

		authHeaderValue = BuildAuthorisation(authChallenge, username, password, "OPTIONS", rtspURL)
		resp, err = conn.Send("OPTIONS", rtspURL, map[string]string{"Authorization": authHeaderValue})
		if err != nil {
			return offlineResult(cameraID, start, fmt.Errorf("authenticated OPTIONS failed: %v", err))
		}
		if resp.StatusCode == 401 {
			return unauthenticatedResult(cameraID, start)
		}
		if resp.StatusCode != 200 {
			return offlineResult(cameraID, start, fmt.Errorf("OPTIONS returned %d", resp.StatusCode))
		}
	} else if resp.StatusCode != 200 {
		return offlineResult(cameraID, start, fmt.Errorf("OPTIONS returned %d", resp.StatusCode))
	}

	// 6. Send DESCRIBE with Accept: application/sdp header
	//    if err or status != 200 → return UNHEALTHY
	//    if body is empty → return UNHEALTHY
	resp, err = conn.Send("DESCRIBE", rtspURL, map[string]string{"Accept": "application/sdp", "Authorization": authHeaderValue})
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to send DESCRIBE: %v", err))
	}
	if resp.StatusCode != 200 {
		return unhealthyResult(cameraID, start, fmt.Errorf("unexpected status code for DESCRIBE: %d", resp.StatusCode))
	}
	if resp.Body == "" {
		return unhealthyResult(cameraID, start, fmt.Errorf("empty body in DESCRIBE response"))
	}

	// 7. Send SETUP
	//    needs Transport header:
	//    "RTP/AVP/TCP;unicast;interleaved=0-1"
	//    parse Session header from response for next step
	resp, err = conn.Send("SETUP", rtspURL+"/trackID=0", map[string]string{
		"Transport":     "RTP/AVP/TCP;unicast;interleaved=0-1",
		"Authorization": authHeaderValue,
	})
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("SETUP failed: %v", err))
	}
	if resp.StatusCode != 200 {
		return unhealthyResult(cameraID, start, fmt.Errorf("SETUP returned %d", resp.StatusCode))
	}

	// NOW parse session from SETUP response
	session := strings.SplitN(resp.Headers["Session"], ";", 2)[0]

	// 8. Send PLAY
	//    needs Session header from SETUP response
	_, err = conn.Send("PLAY", rtspURL, map[string]string{"Session": session, "Authorization": authHeaderValue})
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to send PLAY: %v", err))
	}

	// 9. ReadRTPPacket — if err → return UNHEALTHY
	_, err = conn.ReadRTPPacket()
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to read RTP packet: %v", err))
	}
	// 10. Send TEARDOWN, return HEALTHY
	conn.Send("TEARDOWN", rtspURL, map[string]string{"Session": session, "Authorization": authHeaderValue})
	return HealthResult{
		CameraID: cameraID,
		Status:   StatusHealthy,
		Latency:  time.Since(start),
	}
}
