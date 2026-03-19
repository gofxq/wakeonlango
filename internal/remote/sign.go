package remote

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
)

type signaturePayload struct {
	CommandID  string `json:"command_id"`
	AgentID    string `json:"agent_id"`
	Action     string `json:"action"`
	TargetID   string `json:"target_id"`
	OperatorID string `json:"operator_id"`
	IAT        string `json:"iat"`
	EXP        string `json:"exp"`
	Nonce      string `json:"nonce"`
}

func canonicalPayload(c Command) ([]byte, error) {
	p := signaturePayload{
		CommandID:  c.CommandID,
		AgentID:    c.AgentID,
		Action:     c.Action,
		TargetID:   c.TargetID,
		OperatorID: c.OperatorID,
		IAT:        c.IAT.UTC().Format("2006-01-02T15:04:05Z07:00"),
		EXP:        c.EXP.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Nonce:      c.Nonce,
	}
	return json.Marshal(p)
}

func SignCommand(c *Command, privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid private key size")
	}
	b, err := canonicalPayload(*c)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(privateKey, b)
	c.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

func VerifyCommand(c Command, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid public key size")
	}
	if c.Signature == "" {
		return errors.New("empty signature")
	}
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return err
	}
	b, err := canonicalPayload(c)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, b, sig) {
		return errors.New("invalid signature")
	}
	return nil
}

func SHA256FingerprintDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
