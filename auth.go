package rtsp

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"strings"
)

// When you send OPTIONS without credentials and the camera requires auth, it responds with:
// RTSP/1.0 401 Unauthorized\r\n
// CSeq: 1\r\n
// WWW-Authenticate: Basic realm="camera"\r\n
// \r\n
// Or for Digest auth (more common on real cameras):
// RTSP/1.0 401 Unauthorized\r\n
// CSeq: 1\r\n
// WWW-Authenticate: Digest realm="camera", nonce="abc123"\r\n
// \r\n

// Basic — trivially simple:
// Authorization: Basic base64(username:password)
// Digest — slightly more involved:
// Authorization: Digest username="admin", realm="camera",
//                nonce="abc123", uri="rtsp://...", response="<hash>"
// Where response is:
// HA1 = md5(username:realm:password)
// HA2 = md5(method:uri)
// response = md5(HA1:nonce:HA2)

type AuthChallenge struct {
	Scheme string // "Basic" or "Digest"
	Realm  string
	Nonce  string // Digest only
}

// md5Hash computes the MD5 hash of the input string and returns it as a lowercase hex string.
// Note: MD5 is deprecated for security; used here for Digest auth compatibility per RFC 2617.
func md5Hash(input string) string {
	h := md5.New()
	fmt.Fprintf(h, "%s", input)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Task 1 — ParseAuthChallenge:
// Task 2 — BuildAuthorization:

func ParseAuthChallenge(header string) (*AuthChallenge, error) {
	// header looks like "Basic realm="camera"" or "Digest realm="camera", nonce="abc123""

	// 1. split on first space to get scheme + rest
	// 2. parse key="value" pairs from rest
	//    hint: split on ", " then each part on "="
	// 3. return AuthChallenge

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid WWW-Authenticate header: %s", header)
	}
	scheme := parts[0]
	params := parts[1]
	paramParts := strings.Split(params, ", ")
	paramMap := make(map[string]string)
	for _, p := range paramParts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			key := kv[0]
			value := strings.Trim(kv[1], `"`) // remove quotes
			paramMap[key] = value
		}
	}
	auth := &AuthChallenge{Scheme: scheme}
	if realm, ok := paramMap["realm"]; ok {
		auth.Realm = realm
	}
	if nonce, ok := paramMap["nonce"]; ok {
		auth.Nonce = nonce
	}
	return auth, nil
}

func BuildAuthorisation(auth *AuthChallenge, method, uri, username, password string) string {
	// if Basic: return "Basic " + base64(username+":"+password)
	// if Digest: compute HA1, HA2, response as above, return Digest header string
	if auth.Scheme == "Basic" {
		creds := fmt.Sprintf("%s:%s", username, password)
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
	} else if auth.Scheme == "Digest" {
		ha1 := md5Hash(fmt.Sprintf("%s:%s:%s", username, auth.Realm, password))
		ha2 := md5Hash(fmt.Sprintf("%s:%s", method, uri))
		response := md5Hash(fmt.Sprintf("%s:%s:%s", ha1, auth.Nonce, ha2))
		authHeader := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
			username, auth.Realm, auth.Nonce, uri, response)
		return authHeader
	}
	return ""
}
