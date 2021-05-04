package main

import (
	"log"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/sethvargo/go-signalcontext"
)

func main() {
	cli, err := cloudevents.NewDefaultClient()
	if err != nil {
		log.Panicf("Unable to create CloudEvents client: %s", err)
	}

	h := NewHandler(cli)

	log.Print("Running CloudEvents handler")

	ctx, cancel := signalcontext.OnInterrupt()
	defer cancel()

	if err := h.Run(ctx); err != nil {
		log.Panicf("Failure during runtime of CloudEvents handler: %s", err)
	}
}
