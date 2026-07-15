// Package webhooks carries inbound provider webhooks from the HTTP edge to a
// background worker over Asynq, so the request handler never runs business
// logic inline.
package webhooks

import "time"

const TaskWebhookReceived = "webhook:received"

const queueWebhooks = "webhooks:default"

func QueueNames() []string {
	return []string{queueWebhooks}
}

// ReceivedEvent is the verified webhook handed off to the worker. Payload is
// the raw provider body, kept verbatim for per-provider decoding.
type ReceivedEvent struct {
	Provider   string            `json:"provider"`
	Payload    []byte            `json:"payload"`
	Headers    map[string]string `json:"headers,omitempty"`
	ReceivedAt time.Time         `json:"received_at"`
	RequestID  string            `json:"request_id,omitempty"`
}
