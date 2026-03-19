package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cmd := Command{
		CommandID:  "c1",
		AgentID:    "a1",
		Action:     "wake",
		TargetID:   "pc-1",
		OperatorID: "alice",
		IAT:        time.Now().UTC(),
		EXP:        time.Now().UTC().Add(time.Minute),
		Nonce:      "n1",
	}
	if err := SignCommand(&cmd, priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyCommand(cmd, pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
	cmd.TargetID = "pc-2"
	if err := VerifyCommand(cmd, pub); err == nil {
		t.Fatal("expected verify failure")
	}
}

func TestNonceCache(t *testing.T) {
	cache := NewNonceCache(time.Minute)
	now := time.Now()
	if cache.SeenOrStore("abc", now) {
		t.Fatal("first nonce should not be seen")
	}
	if !cache.SeenOrStore("abc", now.Add(time.Second)) {
		t.Fatal("second nonce should be seen")
	}
}

func TestFixedWindowLimiter(t *testing.T) {
	lim := NewFixedWindowLimiter(time.Minute, 2)
	now := time.Now()
	if !lim.Allow("u1", now) || !lim.Allow("u1", now.Add(time.Second)) {
		t.Fatal("first two requests should pass")
	}
	if lim.Allow("u1", now.Add(2*time.Second)) {
		t.Fatal("third request should be limited")
	}
	if !lim.Allow("u1", now.Add(61*time.Second)) {
		t.Fatal("request should pass after window")
	}
}
