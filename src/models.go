package main

import (
	"encoding/json"
	"time"
)


// Notification types
const (
    SystemAlert        = "system_alert"
    SystemNotification = "system_notification"
    UserMessage        = "user_message"
    AIResponse         = "ai_response"
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
	NotificationID string `json:"notificationId"` 
	TargetTeamID      string `json:"targetTeamId"`
	TargetUserID string `json:"targetUserId"` 
	SenderUserID      string `json:"senderUserId"` 
	MessageType string `json:"messageType"`
	Body     string `json:"body"`
	Timestamp   int64  `json:"timestamp"`
}

// NewMessage creates a new message with the current timestamp
func NewMessage(NotificationID, TargetTeamID, TargetUserID, SenderUserID, MessageType, Body string) *Message {
	return &Message{
		NotificationID: NotificationID,
		TargetTeamID: TargetTeamID,
		TargetUserID: TargetUserID, 
		SenderUserID: SenderUserID, 
		MessageType: MessageType,
		Body:     Body,
		Timestamp:   time.Now().UnixNano() / int64(time.Millisecond),
	}
}

// MessageRequest represents the incoming REST API request
type MessageRequest struct {
	NotificationID string `json:"notification_id"` // Unique ID for the notification
	TargetTeamID       string `json:"target_team_id"`
	SenderUserID       string `json:"sender_user_id"`       // Sender user ID
	TargetUserID string `json:"target_user_id"` // Optional: specific user to send to
	MessageType  string `json:"message_type"`
	Body      string `json:"body"`
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
