package rtsp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type HealthStatus string

const (
	Healthy         HealthStatus = "HEALTHY"
	Unauthenticated HealthStatus = "UNAUTHENTICATED"
	Unhealthy       HealthStatus = "UNHEALTHY"
	Offline         HealthStatus = "OFFLINE"
)

type HealthResult struct {
	CameraID int
	Status   HealthStatus
	Latency  time.Duration
	Error    string // human readable, empty if healthy
}

func authHeaders(authValue string, extra ...map[string]string) map[string]string {
	h := make(map[string]string)
	if authValue != "" {
		h["Authorization"] = authValue
	}
	if len(extra) > 0 {
		for k, v := range extra[0] {
			h[k] = v
		}
	}
	return h
}

func handleAuth(resp *Response, method, url, username, password string) (string, error) {
	if resp.StatusCode != 401 {
		return "", nil
	}
	wwwAuth, ok := resp.Headers["WWW-Authenticate"]
	if !ok {
		return "", fmt.Errorf("401 with no WWW-Authenticate header")
	}
	challenge, err := ParseAuthChallenge(wwwAuth)
	if err != nil {
		return "", fmt.Errorf("failed to parse auth challenge: %v", err)
	}
	return BuildAuthorisation(challenge, username, password, method, url), nil
}

func parseTrackURL(baseURL, sdpBody string) string {
	for _, line := range strings.Split(sdpBody, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=control:") {
			track := strings.TrimPrefix(line, "a=control:")
			// if it's already a full URL, use it directly
			if strings.HasPrefix(track, "rtsp://") {
				return track
			}
			// otherwise append to base URL
			return strings.TrimRight(baseURL, "/") + "/" + track
		}
	}
	// fallback
	return baseURL + "/trackID=0"
}

func offlineResult(id int, start time.Time, err error) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   Offline,
		Latency:  time.Since(start),
		Error:    err.Error(),
	}
}

func unhealthyResult(id int, start time.Time, err error) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   Unhealthy,
		Latency:  time.Since(start),
		Error:    err.Error(),
	}
}

func unauthenticatedResult(id int, start time.Time) HealthResult {
	return HealthResult{
		CameraID: id,
		Status:   Unauthenticated,
		Latency:  time.Since(start),
		Error:    "invalid credentials",
	}
}

func HealthCheck(ctx context.Context, cameraID int, rtspURL string, timeout time.Duration) HealthResult {
	start := time.Now()
	// 1. parse credentials out of the URL
	//    hint: use net/url — url.Parse(rtspURL) gives you u.User.Username() and u.User.Password()
	u, err := url.Parse(rtspURL)
	if err != nil {
		return offlineResult(cameraID, start, fmt.Errorf("invalid URL: %v", err))
	}
	username := u.User.Username()
	password, _ := u.User.Password()

	// proactively build Basic auth if credentials are present in the URL
	var authHeaderValue string
	if username != "" {
		creds := fmt.Sprintf("%s:%s", username, password)
		authHeaderValue = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
	}

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
	resp, err := conn.Send("OPTIONS", rtspURL, authHeaders(authHeaderValue))
	if err != nil {
		return offlineResult(cameraID, start, fmt.Errorf("OPTIONS failed: %v", err))
	}
	if resp.StatusCode == 401 {
		// 5. (auth) parse WWW-Authenticate header
		//    call BuildAuthorisation
		//    retry OPTIONS with Authorization header
		//    if still 401 → return UNAUTHENTICATED
		//    if err → return OFFLINE
		authHeaderValue, err = handleAuth(resp, "OPTIONS", rtspURL, username, password)
		if err != nil {
			return unauthenticatedResult(cameraID, start)
		}
		resp, err = conn.Send("OPTIONS", rtspURL, authHeaders(authHeaderValue))
		if err != nil {
			return offlineResult(cameraID, start, fmt.Errorf("authenticated OPTIONS failed: %v", err))
		}
		if resp.StatusCode == 401 {
			return unauthenticatedResult(cameraID, start)
		}
	}
	if resp.StatusCode != 200 {
		return offlineResult(cameraID, start, fmt.Errorf("OPTIONS returned %d", resp.StatusCode))
	}

	// 6. Send DESCRIBE with Accept: application/sdp header
	//    if err or status != 200 → return UNHEALTHY
	//    if body is empty → return UNHEALTHY
	resp, err = conn.Send("DESCRIBE", rtspURL, authHeaders(authHeaderValue, map[string]string{"Accept": "application/sdp"}))
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to send DESCRIBE: %v", err))
	}
	if resp.StatusCode != 200 {
		return unhealthyResult(cameraID, start, fmt.Errorf("unexpected status code for DESCRIBE: %d", resp.StatusCode))
	}
	if resp.Body == "" {
		return unhealthyResult(cameraID, start, fmt.Errorf("empty body in DESCRIBE response"))
	}

	trackURL := parseTrackURL(rtspURL, resp.Body)
	// 7. Send SETUP
	//    needs Transport header:
	//    "RTP/AVP/TCP;unicast;interleaved=0-1"
	//    parse Session header from response for next step
	resp, err = conn.Send("SETUP", trackURL, authHeaders(authHeaderValue, map[string]string{"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1"}))
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
	_, err = conn.Send("PLAY", rtspURL, authHeaders(authHeaderValue, map[string]string{"Session": session}))
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to send PLAY: %v", err))
	}

	// 9. ReadRTPPacket — if err → return UNHEALTHY
	_, err = conn.ReadRTPPacket()
	if err != nil {
		return unhealthyResult(cameraID, start, fmt.Errorf("failed to read RTP packet: %v", err))
	}
	// 10. Send TEARDOWN, return HEALTHY
	conn.Send("TEARDOWN", rtspURL, authHeaders(authHeaderValue, map[string]string{"Session": session}))
	return HealthResult{
		CameraID: cameraID,
		Status:   Healthy,
		Latency:  time.Since(start),
	}
}
