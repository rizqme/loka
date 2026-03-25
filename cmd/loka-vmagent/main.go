package main

import (
	"log"

	"github.com/vyprai/loka/internal/vmagent"
)

func main() {
	agent, err := vmagent.ListenVsock()
	if err != nil {
		log.Fatalf("failed to start vmagent: %v", err)
	}
	defer agent.Close()

	// Start vsock-to-TCP port relays so the host can reach lokad
	// via vsock without lokad needing to listen on vsock directly.
	// vsock:6840 -> TCP localhost:6840 (HTTP API)
	// vsock:6841 -> TCP localhost:6841 (gRPC)
	if err := agent.StartPortRelay(6840, 6840); err != nil {
		log.Printf("warning: port relay 6840 failed: %v", err)
	}
	if err := agent.StartPortRelay(6841, 6841); err != nil {
		log.Printf("warning: port relay 6841 failed: %v", err)
	}

	log.Printf("loka-vmagent listening on vsock:%d", vmagent.VsockPort)
	log.Printf("port relays: vsock:6840->tcp:6840, vsock:6841->tcp:6841")
	if err := agent.Serve(); err != nil {
		log.Fatalf("vmagent serve error: %v", err)
	}
}
