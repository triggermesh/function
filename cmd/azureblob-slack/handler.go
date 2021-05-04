package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

const (
	eventTypeSlack = "com.slack.webapi.chat.postMessage"
	eventSrcName   = "/apis/flow.triggermesh.io/v1alpha1/namespaces/antoineco/functions/azureblob-slack-message"

	slackChannel = "C0217LQP3T3"
)

// Handler runs a CloudEvents receiver.
type Handler struct {
	cli cloudevents.Client
}

// NewHandler returns a new Handler for the given CloudEvents client.
func NewHandler(c cloudevents.Client) *Handler {
	rand.Seed(time.Now().UnixNano())

	return &Handler{
		cli: c,
	}
}

// Run starts the handler and blocks until it returns.
func (h *Handler) Run(ctx context.Context) error {
	return h.cli.StartReceiver(ctx, h.receive)
}

// SlackResponse represents the data of a transformed Azure Blob Storage CloudEvent.
type SlackResponse struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// receive implements the handler's receive logic.
func (h *Handler) receive(e cloudevents.Event) (*cloudevents.Event, cloudevents.Result) {
	data := make(map[string]interface{})
	if err := e.DataAs(&data); err != nil {
		return nil, cloudevents.ResultNACK
	}

	eventName := data["api"].(string)
	blob := e.Subject()

	text := fmt.Sprintf("Event from Azure Blob Storage: `%s`\n"+
		"Blob: `%s`",
		eventName, blob)

	return newSlackEvent(text), cloudevents.ResultACK
}

// newSlackEvent returns a Slack CloudEvent.
func newSlackEvent(text string) *cloudevents.Event {
	resp := cloudevents.NewEvent()
	resp.SetType(eventTypeSlack)
	resp.SetSource(eventSrcName)

	data := &SlackResponse{
		Channel: slackChannel,
		Text:    text,
	}

	if err := resp.SetData(cloudevents.ApplicationJSON, data); err != nil {
		log.Panicf("Error serializing CloudEvent data: %s", err)
	}

	return &resp
}
