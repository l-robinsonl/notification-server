package main

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AuthMessage struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
	TeamID string `json:"teamId"`
	Token  string `json:"token"`
}

func (a *AuthMessage) Normalize() {
	a.Type = strings.TrimSpace(a.Type)
	a.UserID = strings.TrimSpace(a.UserID)
	a.TeamID = strings.TrimSpace(a.TeamID)
	a.Token = strings.TrimSpace(a.Token)
}

// Message represents a notification delivered to websocket clients.
type Message struct {
	NotificationID string `json:"notificationId"`
	TargetTeamID   string `json:"targetTeamId"`
	TargetUserID   string `json:"targetUserId"`
	SenderUserID   string `json:"senderUserId"`
	MessageType    string `json:"messageType"`
	Body           string `json:"body"`
	Timestamp      int64  `json:"timestamp"`
}

// NewMessage creates a new message with the current timestamp
func NewMessage(notificationID, targetTeamID, targetUserID, senderUserID, messageType, body string) *Message {
	return &Message{
		NotificationID: notificationID,
		TargetTeamID:   targetTeamID,
		TargetUserID:   targetUserID,
		SenderUserID:   senderUserID,
		MessageType:    messageType,
		Body:           body,
		Timestamp:      time.Now().UnixMilli(),
	}
}

// MessageRequest represents the incoming REST API request
type MessageRequest struct {
	NotificationID string `json:"notification_id"` // Unique ID for the notification
	TargetTeamID   string `json:"target_team_id"`
	SenderUserID   string `json:"sender_user_id"` // Sender user ID
	TargetUserID   string `json:"target_user_id"`
	MessageType    string `json:"message_type"`
	Body           string `json:"body"`
	Broadcast      bool   `json:"broadcast"`
}

// ToJSON converts a message to JSON bytes (camelCase for WebSocket)
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

func (r *MessageRequest) Normalize() {
	r.NotificationID = strings.TrimSpace(r.NotificationID)
	r.TargetTeamID = strings.TrimSpace(r.TargetTeamID)
	r.SenderUserID = strings.TrimSpace(r.SenderUserID)
	r.TargetUserID = strings.TrimSpace(r.TargetUserID)
	r.MessageType = strings.TrimSpace(r.MessageType)
	r.Body = strings.TrimSpace(r.Body)
}

func (r *MessageRequest) Validate() error {
	if r.MessageType == "" {
		return errors.New("missing required field: message_type")
	}

	if strings.TrimSpace(r.Body) == "" {
		return errors.New("missing required field: body")
	}

	if r.Broadcast {
		if r.TargetUserID != "" {
			return errors.New("cannot specify target_user_id when broadcast is true")
		}
		return nil
	}

	if r.TargetUserID == "" {
		return errors.New("must specify target_user_id for non-broadcast messages")
	}

	return nil
}
