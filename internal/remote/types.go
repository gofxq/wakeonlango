package remote

import "time"

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Command struct {
	CommandID  string    `json:"command_id"`
	AgentID    string    `json:"agent_id"`
	Action     string    `json:"action"`
	TargetID   string    `json:"target_id"`
	OperatorID string    `json:"operator_id"`
	IAT        time.Time `json:"iat"`
	EXP        time.Time `json:"exp"`
	Nonce      string    `json:"nonce"`
	Signature  string    `json:"signature"`
}

type CommandEnvelope struct {
	Command Command `json:"command"`
}

type CommandRequest struct {
	AgentID  string `json:"agent_id"`
	TargetID string `json:"target_id"`
}

type CommandAck struct {
	CommandID string `json:"command_id"`
	AgentID   string `json:"agent_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
}
