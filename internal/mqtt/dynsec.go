package mqtt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DynsecClient manages Mosquitto Dynamic Security users via the MQTT control API.
// Commands are published to $CONTROL/dynamic-security/v1 and responses arrive on
// $CONTROL/dynamic-security/v1/response.  All operations are serialised through a
// mutex so only one command is in-flight at a time.
type DynsecClient struct {
	client     *Client
	mu         sync.Mutex
	responseCh chan dynsecResponse
}

type dynsecCmd struct {
	Commands []map[string]interface{} `json:"commands"`
}

type dynsecResponse struct {
	Responses []struct {
		Command         string `json:"command"`
		CorrelationData string `json:"correlationData,omitempty"`
		Error           string `json:"error,omitempty"`
	} `json:"responses"`
}

// NewDynsecClient wraps an existing connected MQTT client with dynamic-security
// command support.  It subscribes to the response topic immediately.
func NewDynsecClient(c *Client) *DynsecClient {
	d := &DynsecClient{client: c}

	_ = c.Subscribe("$CONTROL/dynamic-security/v1/response", func(_ string, payload []byte) {
		var resp dynsecResponse
		if json.Unmarshal(payload, &resp) != nil {
			return
		}
		d.mu.Lock()
		ch := d.responseCh
		d.mu.Unlock()
		if ch != nil {
			select {
			case ch <- resp:
			default:
			}
		}
	})
	return d
}

// send publishes a batch of commands and waits for the response.
// correlationData is used to identify our response in concurrent environments
// (only one goroutine can call send at a time due to the outer lock).
func (d *DynsecClient) send(commands []map[string]interface{}, correlationID string) error {
	// Attach correlationData to every command
	for _, cmd := range commands {
		cmd["correlationData"] = correlationID
	}

	ch := make(chan dynsecResponse, 1)
	d.responseCh = ch

	data, err := json.Marshal(dynsecCmd{Commands: commands})
	if err != nil {
		d.responseCh = nil
		return err
	}

	if err := d.client.Publish("$CONTROL/dynamic-security/v1", data); err != nil {
		d.responseCh = nil
		return fmt.Errorf("dynsec publish: %w", err)
	}

	select {
	case resp := <-ch:
		d.responseCh = nil
		for _, r := range resp.Responses {
			if r.Error != "" {
				return fmt.Errorf("dynsec %s: %s", r.Command, r.Error)
			}
		}
		return nil
	case <-time.After(5 * time.Second):
		d.responseCh = nil
		return fmt.Errorf("dynsec command timed out (correlationData=%s)", correlationID)
	}
}

// CreateOrgUser provisions an MQTT user for an organisation.
// The user may publish/subscribe to:
//   - data/{orgName}/#   — tag data scoped to this org
//   - sys/#              — health/write/reload (authenticated access)
//   - spBv1.0/#          — Sparkplug B (authenticated access)
func (d *DynsecClient) CreateOrgUser(orgID int, orgName, username, password string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)
	dataPrefix := fmt.Sprintf("data/%s/#", orgName)

	return d.send([]map[string]interface{}{
		{
			"command":  "createRole",
			"rolename": roleName,
			"acls": []map[string]interface{}{
				{"acltype": "publishClientSend", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "publishClientSend", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "publishClientSend", "topic": "spBv1.0/#", "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": "spBv1.0/#", "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": "spBv1.0/#", "priority": 1, "allow": true},
			},
		},
		{
			"command":  "createClient",
			"username": username,
			"password": password,
			"roles":    []map[string]interface{}{{"rolename": roleName}},
		},
	}, corrID)
}

// DeleteOrgUser removes an MQTT user and their dedicated role.
func (d *DynsecClient) DeleteOrgUser(orgID int, username string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)

	return d.send([]map[string]interface{}{
		{"command": "deleteClient", "username": username},
		{"command": "deleteRole", "rolename": roleName},
	}, corrID)
}

// UpdateOrgRole updates the ACL topic prefix for an org's role (e.g. after rename).
func (d *DynsecClient) UpdateOrgRole(orgID int, newOrgName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	corrID := newCorrID()
	roleName := fmt.Sprintf("org-%d-role", orgID)
	dataPrefix := fmt.Sprintf("data/%s/#", newOrgName)

	return d.send([]map[string]interface{}{
		{
			"command":  "modifyRole",
			"rolename": roleName,
			"acls": []map[string]interface{}{
				{"acltype": "publishClientSend", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": dataPrefix, "priority": 1, "allow": true},
				{"acltype": "publishClientSend", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": "sys/#", "priority": 1, "allow": true},
				{"acltype": "publishClientSend", "topic": "spBv1.0/#", "priority": 1, "allow": true},
				{"acltype": "publishClientReceive", "topic": "spBv1.0/#", "priority": 1, "allow": true},
				{"acltype": "subscribePattern", "topic": "spBv1.0/#", "priority": 1, "allow": true},
			},
		},
	}, corrID)
}

// GeneratePassword returns a random 32-character hex password suitable for MQTT credentials.
func GeneratePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newCorrID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
