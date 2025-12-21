// Package common provides shared types and utilities for server implementations.
package common

import (
	"encoding/json"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// EventToJSONRPCNotification converts a domain event to a JSON-RPC notification.
// This is used to send events to JSON-RPC clients.
func EventToJSONRPCNotification(event events.Event) ([]byte, error) {
	method := "event/" + string(event.Type())

	// Get event JSON to extract payload
	data, err := event.ToJSON()
	if err != nil {
		return nil, err
	}

	// Extract payload from event structure
	payload, err := ExtractEventPayload(data)
	if err != nil {
		return nil, err
	}

	// Create JSON-RPC notification
	notification, err := message.NewNotification(method, payload)
	if err != nil {
		return nil, err
	}

	return json.Marshal(notification)
}

// ExtractEventPayload extracts the payload field from event JSON.
func ExtractEventPayload(eventJSON []byte) (interface{}, error) {
	var eventData struct {
		Event     string      `json:"event"`
		Timestamp string      `json:"timestamp"`
		Payload   interface{} `json:"payload"`
	}
	if err := json.Unmarshal(eventJSON, &eventData); err != nil {
		return nil, err
	}
	return eventData.Payload, nil
}

// EventNotificationMethod returns the JSON-RPC method name for an event type.
func EventNotificationMethod(eventType events.EventType) string {
	return "event/" + string(eventType)
}
