package main

import (
	"encoding/json"
	"time"
)


type AuthMessage struct {
  Type   string `json:"type"`
  UserID string    `json:"user_id"`
  TeamID string `json:"team_id"`
  Token  string `json:"token"`
}

type UserData struct {
  ID          int    `json:"id"`
  Email       string `json:"email"`

}

// Message represents a message sent between clients
type Message struct {
	TeamID      string `json:"team_id"`
	UserID      string `json:"user_id"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
	Timestamp   int64  `json:"timestamp"`
}

// NewMessage creates a new message with the current timestamp
func NewMessage(teamID, userID, messageType, content string) *Message {
	return &Message{
		TeamID:      teamID,
		UserID:      userID,
		MessageType: messageType,
		Content:     content,
		Timestamp:   time.Now().UnixNano() / int64(time.Millisecond),
	}
}

// MessageRequest represents the incoming REST API request
type MessageRequest struct {
	TeamID       string `json:"team_id"`
	UserID       string `json:"user_id"`       // Sender user ID
	TargetUserID string `json:"target_user_id"` // Optional: specific user to send to
	MessageType  string `json:"message_type"`
	Content      string `json:"content"`
	Broadcast    bool   `json:"broadcast"` // Whether to broadcast to the entire team
}

// ToJSON converts a message to JSON bytes
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// FromJSON parses JSON bytes into a MessageRequest
func MessageRequestFromJSON(data []byte) (*MessageRequest, error) {
	var req MessageRequest
	err := json.Unmarshal(data, &req)
	return &req, err
}
