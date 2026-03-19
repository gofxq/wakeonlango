package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"wakego/internal/config"
	"wakego/internal/remote"
	"wakego/internal/wol"
)

type agentConfig struct {
	RemoteBaseURL            string   `json:"remote_base_url"`
	AgentID                  string   `json:"agent_id"`
	AgentToken               string   `json:"agent_token"`
	PinnedServerCertSHA256   string   `json:"pinned_server_cert_sha256"`
	ServerPublicKeyBase64    string   `json:"server_public_key_base64"`
	AllowedDeviceIDs         []string `json:"allowed_device_ids"`
	PollIntervalMilliseconds int      `json:"poll_interval_ms"`
}

func main() {
	var (
		agentCfgPath string
		localCfgPath string
	)
	flag.StringVar(&agentCfgPath, "agent-config", "agent-config.json", "agent config path")
	flag.StringVar(&localCfgPath, "config", "config.json", "local wake config path")
	flag.Parse()

	agentCfg, err := loadAgentConfig(agentCfgPath)
	if err != nil {
		log.Fatalf("load agent config: %v", err)
	}
	store, err := config.NewStore(localCfgPath)
	if err != nil {
		log.Fatalf("load local config: %v", err)
	}

	pubBytes, err := base64.StdEncoding.DecodeString(agentCfg.ServerPublicKeyBase64)
	if err != nil {
		log.Fatalf("decode server public key: %v", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		log.Fatalf("server public key size must be %d", ed25519.PublicKeySize)
	}

	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, VerifyConnection: pinVerify(agentCfg.PinnedServerCertSHA256)}}}
	allow := map[string]struct{}{}
	for _, id := range agentCfg.AllowedDeviceIDs {
		allow[strings.TrimSpace(id)] = struct{}{}
	}

	nonce := remote.NewNonceCache(5 * time.Minute)
	logger := log.New(os.Stdout, "[wakeagent] ", log.LstdFlags|log.Lmicroseconds)

	for {
		cmd, hasCmd, err := pullCommand(client, agentCfg)
		if err != nil {
			logger.Printf("pull command failed: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		if !hasCmd {
			time.Sleep(time.Duration(agentCfg.PollIntervalMilliseconds) * time.Millisecond)
			continue
		}

		success, msg := executeCommand(cmd, ed25519.PublicKey(pubBytes), nonce, allow, store)
		if !success {
			logger.Printf("ALERT command failed id=%s reason=%s", cmd.CommandID, msg)
		}
		if err := sendAck(client, agentCfg, remote.CommandAck{CommandID: cmd.CommandID, AgentID: agentCfg.AgentID, Success: success, Message: msg}); err != nil {
			logger.Printf("send ack failed command=%s err=%v", cmd.CommandID, err)
		}
	}
}

func loadAgentConfig(path string) (agentConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return agentConfig{}, err
	}
	var c agentConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return agentConfig{}, err
	}
	if c.RemoteBaseURL == "" || c.AgentID == "" || c.AgentToken == "" || c.PinnedServerCertSHA256 == "" || c.ServerPublicKeyBase64 == "" {
		return agentConfig{}, errors.New("remote_base_url/agent_id/agent_token/pinned_server_cert_sha256/server_public_key_base64 are required")
	}
	if c.PollIntervalMilliseconds <= 0 {
		c.PollIntervalMilliseconds = 1000
	}
	return c, nil
}

func pinVerify(pinnedHex string) func(tls.ConnectionState) error {
	pinnedHex = strings.ToLower(strings.TrimSpace(pinnedHex))
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("missing peer certificates")
		}
		leaf := cs.PeerCertificates[0]
		sum := sha256.Sum256(leaf.Raw)
		if hex.EncodeToString(sum[:]) != pinnedHex {
			return fmt.Errorf("tls pin mismatch")
		}
		return nil
	}
}

func pullCommand(client *http.Client, c agentConfig) (remote.Command, bool, error) {
	url := fmt.Sprintf("%s/api/v1/agent/pull?agent_id=%s", strings.TrimRight(c.RemoteBaseURL, "/"), c.AgentID)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Agent-Token", c.AgentToken)
	resp, err := client.Do(req)
	if err != nil {
		return remote.Command{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return remote.Command{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return remote.Command{}, false, fmt.Errorf("status=%d", resp.StatusCode)
	}
	var out struct {
		OK      bool           `json:"ok"`
		Command remote.Command `json:"command"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return remote.Command{}, false, err
	}
	return out.Command, true, nil
}

func executeCommand(cmd remote.Command, pub ed25519.PublicKey, nonce *remote.NonceCache, allow map[string]struct{}, store *config.Store) (bool, string) {
	if cmd.Action != "wake" {
		return false, "unsupported action"
	}
	if err := remote.VerifyCommand(cmd, pub); err != nil {
		return false, "signature verify failed: " + err.Error()
	}
	now := time.Now().UTC()
	if now.After(cmd.EXP) || now.Before(cmd.IAT.Add(-10*time.Second)) {
		return false, "command expired or not yet valid"
	}
	if nonce.SeenOrStore(cmd.Nonce, now) {
		return false, "replayed nonce"
	}
	if _, ok := allow[cmd.TargetID]; !ok {
		return false, "target not in allowlist"
	}
	device, ok := store.GetDevice(cmd.TargetID)
	if !ok {
		return false, "target device not found"
	}
	if err := wol.Send(device.MAC, device.Broadcast, device.Port); err != nil {
		return false, "wake failed: " + err.Error()
	}
	return true, "wake sent"
}

func sendAck(client *http.Client, c agentConfig, ack remote.CommandAck) error {
	url := strings.TrimRight(c.RemoteBaseURL, "/") + "/api/v1/agent/ack"
	b, _ := json.Marshal(ack)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", c.AgentToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status=%d", resp.StatusCode)
	}
	return nil
}
