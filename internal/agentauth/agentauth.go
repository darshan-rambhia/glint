// Package agentauth signs and verifies the requests a `glint agent` sends.
//
// Each agent shares a secret with the server. A request carries the agent
// name, a unix timestamp, a random nonce and an HMAC-SHA256 over
//
//	glint-agent-v1\n<METHOD>\n<path>\n<timestamp>\n<nonce>\n<hex sha256(body)>
//
// so the body, target and time are all bound to the signature. HMAC rather
// than a bearer token because TLS usually ends at a reverse proxy in front of
// Glint; the signature still proves nothing on the inner hops changed or
// replayed the report.
package agentauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Header names carried on every agent request.
const (
	HeaderAgent     = "X-Glint-Agent"
	HeaderTimestamp = "X-Glint-Timestamp"
	HeaderNonce     = "X-Glint-Nonce"
	HeaderSignature = "X-Glint-Signature"
)

// MinSecretLen is the shortest secret accepted on either side.
const MinSecretLen = 32

// MaxSkew is how far a request timestamp may drift from the server clock.
const MaxSkew = 5 * time.Minute

// Verification failures. Callers should not tell clients which one occurred.
var (
	ErrMissingHeaders = errors.New("missing auth headers")
	ErrUnknownAgent   = errors.New("unknown agent")
	ErrBadTimestamp   = errors.New("timestamp outside allowed skew")
	ErrBadSignature   = errors.New("signature mismatch")
	ErrReplay         = errors.New("nonce already used")
)

func canonical(method, path, ts, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return fmt.Appendf(nil, "glint-agent-v1\n%s\n%s\n%s\n%s\n%s", method, path, ts, nonce, hex.EncodeToString(sum[:]))
}

func mac(secret string, msg []byte) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(msg)
	return h.Sum(nil)
}

// Sign sets the agent auth headers on req for the given body.
func Sign(req *http.Request, agent, secret string, body []byte, now time.Time) error {
	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		return fmt.Errorf("generating nonce: %w", err)
	}
	nonce := hex.EncodeToString(nb)
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := mac(secret, canonical(req.Method, req.URL.Path, ts, nonce, body))

	req.Header.Set(HeaderAgent, agent)
	req.Header.Set(HeaderTimestamp, ts)
	req.Header.Set(HeaderNonce, nonce)
	req.Header.Set(HeaderSignature, hex.EncodeToString(sig))
	return nil
}

// Verifier checks signed agent requests against a fixed set of secrets.
type Verifier struct {
	secrets map[string]string
	now     func() time.Time

	mu     sync.Mutex
	nonces map[string]time.Time // agent/nonce -> expiry
}

// NewVerifier returns a Verifier for the given agent name -> secret map.
func NewVerifier(secrets map[string]string) *Verifier {
	return &Verifier{
		secrets: secrets,
		now:     time.Now,
		nonces:  make(map[string]time.Time),
	}
}

// Verify authenticates req with its already-read body and returns the agent
// name. A nonce is only consumed once the signature has checked out, so
// unauthenticated traffic cannot fill the replay cache.
func (v *Verifier) Verify(req *http.Request, body []byte) (string, error) {
	agent := req.Header.Get(HeaderAgent)
	ts := req.Header.Get(HeaderTimestamp)
	nonce := req.Header.Get(HeaderNonce)
	sigHex := req.Header.Get(HeaderSignature)
	if agent == "" || ts == "" || nonce == "" || sigHex == "" || len(nonce) > 64 {
		return "", ErrMissingHeaders
	}

	secret, ok := v.secrets[agent]
	if !ok {
		return "", ErrUnknownAgent
	}

	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", ErrBadTimestamp
	}
	now := v.now()
	if d := now.Sub(time.Unix(sec, 0)); d > MaxSkew || d < -MaxSkew {
		return "", ErrBadTimestamp
	}

	got, err := hex.DecodeString(sigHex)
	if err != nil || !hmac.Equal(got, mac(secret, canonical(req.Method, req.URL.Path, ts, nonce, body))) {
		return "", ErrBadSignature
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	for k, exp := range v.nonces {
		if now.After(exp) {
			delete(v.nonces, k)
		}
	}
	key := agent + "/" + nonce
	if _, seen := v.nonces[key]; seen {
		return "", ErrReplay
	}
	// Anything older than MaxSkew is rejected by the timestamp check, so a
	// nonce only needs remembering for the rest of the skew window.
	v.nonces[key] = now.Add(2 * MaxSkew)
	return agent, nil
}
