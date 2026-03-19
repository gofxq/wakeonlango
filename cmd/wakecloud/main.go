package main

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"wakego/internal/remote"
)

type user struct {
	ID    string      `json:"id"`
	Token string      `json:"token"`
	Role  remote.Role `json:"role"`
}

type agent struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type cfg struct {
	ListenAddr        string  `json:"listen_addr"`
	TLSCertFile       string  `json:"tls_cert_file"`
	TLSKeyFile        string  `json:"tls_key_file"`
	EnableMTLS        bool    `json:"enable_mtls"`
	ClientCAFile      string  `json:"client_ca_file"`
	CommandTTLSeconds int     `json:"command_ttl_seconds"`
	PrivateKeyBase64  string  `json:"private_key_base64"`
	Users             []user  `json:"users"`
	Agents            []agent `json:"agents"`
	AuditLogFile      string  `json:"audit_log_file"`
}

type remoteServer struct {
	mu         sync.Mutex
	queues     map[string][]remote.Command
	acks       map[string]remote.CommandAck
	users      map[string]user
	agents     map[string]agent
	logger     *log.Logger
	audit      *os.File
	privateKey ed25519.PrivateKey
	ttl        time.Duration
	userRL     *remote.FixedWindowLimiter
	agentRL    *remote.FixedWindowLimiter
	ipRL       *remote.FixedWindowLimiter
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "cloud-config.json", "cloud config path")
	flag.Parse()

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := log.New(os.Stdout, "[wakecloud] ", log.LstdFlags|log.Lmicroseconds)
	auditFile, err := os.OpenFile(cfg.AuditLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("open audit log: %v", err)
	}
	defer auditFile.Close()

	privBytes, err := base64.StdEncoding.DecodeString(cfg.PrivateKeyBase64)
	if err != nil {
		log.Fatalf("decode private_key_base64: %v", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		log.Fatalf("private_key_base64 size must be %d", ed25519.PrivateKeySize)
	}

	srv := &remoteServer{
		queues:     map[string][]remote.Command{},
		acks:       map[string]remote.CommandAck{},
		users:      map[string]user{},
		agents:     map[string]agent{},
		logger:     logger,
		audit:      auditFile,
		privateKey: ed25519.PrivateKey(privBytes),
		ttl:        time.Duration(cfg.CommandTTLSeconds) * time.Second,
		userRL:     remote.NewFixedWindowLimiter(time.Minute, 60),
		agentRL:    remote.NewFixedWindowLimiter(time.Minute, 120),
		ipRL:       remote.NewFixedWindowLimiter(time.Minute, 240),
	}
	for _, u := range cfg.Users {
		srv.users[u.Token] = u
	}
	for _, a := range cfg.Agents {
		srv.agents[a.Token] = a
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/v1/commands", srv.handleCreateCommand)
	mux.HandleFunc("/api/v1/agent/pull", srv.handleAgentPull)
	mux.HandleFunc("/api/v1/agent/ack", srv.handleAgentAck)
	mux.HandleFunc("/api/v1/commands/ack", srv.handleReadAck)

	httpSrv := &http.Server{Addr: cfg.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	logger.Printf("starting wakecloud on %s", cfg.ListenAddr)

	if cfg.EnableMTLS {
		tlsConfig, err := buildMTLSConfig(cfg)
		if err != nil {
			log.Fatalf("build mtls config: %v", err)
		}
		httpSrv.TLSConfig = tlsConfig
		log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile))
	}
	log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile))
}

func loadConfig(path string) (cfg, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg{}, err
	}
	var c cfg
	if err := json.Unmarshal(b, &c); err != nil {
		return cfg{}, err
	}
	if c.ListenAddr == "" || c.TLSCertFile == "" || c.TLSKeyFile == "" || c.AuditLogFile == "" {
		return cfg{}, errors.New("listen_addr/tls_cert_file/tls_key_file/audit_log_file are required")
	}
	if c.CommandTTLSeconds <= 0 {
		c.CommandTTLSeconds = 60
	}
	if len(c.Users) == 0 || len(c.Agents) == 0 {
		return cfg{}, errors.New("users and agents must not be empty")
	}
	return c, nil
}

func buildMTLSConfig(c cfg) (*tls.Config, error) {
	caPem, err := os.ReadFile(c.ClientCAFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPem) {
		return nil, errors.New("append client ca failed")
	}
	return &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS13}, nil
}

func (s *remoteServer) handleCreateCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := s.authUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role != remote.RoleOperator && u.Role != remote.RoleAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.allow("user:"+u.ID, r.RemoteAddr, true) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
		return
	}

	var req remote.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.TargetID = strings.TrimSpace(req.TargetID)
	if req.AgentID == "" || req.TargetID == "" {
		http.Error(w, "agent_id and target_id required", http.StatusBadRequest)
		return
	}

	cmd := remote.Command{
		CommandID:  fmt.Sprintf("cmd_%d", time.Now().UnixNano()),
		AgentID:    req.AgentID,
		Action:     "wake",
		TargetID:   req.TargetID,
		OperatorID: u.ID,
		IAT:        time.Now().UTC(),
		EXP:        time.Now().UTC().Add(s.ttl),
		Nonce:      fmt.Sprintf("nonce_%d", time.Now().UnixNano()),
	}
	if err := remote.SignCommand(&cmd, s.privateKey); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.queues[req.AgentID] = append(s.queues[req.AgentID], cmd)
	s.mu.Unlock()

	s.auditLog("command_created", map[string]string{"operator": u.ID, "agent": req.AgentID, "target": req.TargetID, "command_id": cmd.CommandID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "command": cmd})
}

func (s *remoteServer) handleAgentPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a, ok := s.authAgent(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.allow("agent:"+a.ID, r.RemoteAddr, false) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
		return
	}

	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentID == "" || agentID != a.ID {
		http.Error(w, "invalid agent_id", http.StatusBadRequest)
		return
	}

	deadline := time.Now().Add(25 * time.Second)
	for {
		s.mu.Lock()
		q := s.queues[agentID]
		if len(q) > 0 {
			cmd := q[0]
			s.queues[agentID] = q[1:]
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "command": cmd})
			return
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *remoteServer) handleAgentAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a, ok := s.authAgent(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var ack remote.CommandAck
	if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ack.CommandID == "" || ack.AgentID != a.ID {
		http.Error(w, "invalid ack", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.acks[ack.CommandID] = ack
	s.mu.Unlock()
	s.auditLog("command_ack", map[string]string{"agent": ack.AgentID, "command_id": ack.CommandID, "success": fmt.Sprintf("%v", ack.Success), "message": ack.Message})
	if !ack.Success {
		s.logger.Printf("ALERT wake command failed command_id=%s agent=%s msg=%s", ack.CommandID, ack.AgentID, ack.Message)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *remoteServer) handleReadAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := s.authUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role == remote.RoleViewer || u.Role == remote.RoleOperator || u.Role == remote.RoleAdmin {
		id := r.URL.Query().Get("command_id")
		s.mu.Lock()
		ack, found := s.acks[id]
		s.mu.Unlock()
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ack": ack})
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}

func (s *remoteServer) authUser(r *http.Request) (user, bool) {
	token := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	u, ok := s.users[token]
	return u, ok
}

func (s *remoteServer) authAgent(r *http.Request) (agent, bool) {
	token := strings.TrimSpace(r.Header.Get("X-Agent-Token"))
	a, ok := s.agents[token]
	return a, ok
}

func (s *remoteServer) allow(subject, remoteAddr string, isUser bool) bool {
	now := time.Now()
	ip := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		ip = host
	}
	if !s.ipRL.Allow("ip:"+ip, now) {
		return false
	}
	if isUser {
		return s.userRL.Allow(subject, now)
	}
	return s.agentRL.Allow(subject, now)
}

func (s *remoteServer) auditLog(event string, values map[string]string) {
	record := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "event": event, "fields": values}
	b, _ := json.Marshal(record)
	_, _ = s.audit.Write(append(b, '\n'))
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
