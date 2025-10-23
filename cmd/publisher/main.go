package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
	stan "github.com/nats-io/stan.go"
)

func main() {
	var (
		file      = flag.String("file", "/data/model.json", "path to JSON file to publish")
		clusterID = flag.String("cluster", "test-cluster", "stan cluster id")
		clientID  = flag.String("client", fmt.Sprintf("publisher-%d", time.Now().UnixNano()), "stan client id")
		channel   = flag.String("channel", "orders", "stan channel")
		natsURL   = flag.String("nats", "nats://nats-streaming:4222", "nats url")
	)
	flag.Parse()

	payload, err := os.ReadFile(*file)
	if err != nil { log.Fatalf("read file: %v", err) }

	sc, err := stan.Connect(*clusterID, *clientID, stan.NatsURL(*natsURL))
	if err != nil { log.Fatalf("stan connect: %v", err) }
	defer sc.Close()

	if err := sc.Publish(*channel, payload); err != nil { log.Fatalf("publish: %v", err) }
	log.Printf("published %d bytes to channel %q", len(payload), *channel)
}