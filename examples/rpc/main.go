package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/rpc"
)

func main() {
	var stopOnce sync.Once
	stopChan := make(chan struct{}, 1)

	// Create an ngrok agent with RPC handler
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")),
		ngrok.WithRPCHandler(func(ctx context.Context, session ngrok.AgentSession, req rpc.Request) ([]byte, error) {
			// Handle different RPC methods
			switch req.Method() {
			case rpc.StopAgentMethod:
				stopOnce.Do(func() {
					go func() {
						// wait a second to ensure that the process has time to
						// respond to the server before shutting down
						time.Sleep(time.Second)
						close(stopChan)
					}()
				})
				// In a real application, you might want to do some cleanup
				// Return nil error to acknowledge the command
				return nil, nil

			case rpc.RestartAgentMethod:
				log.Println("Received restart command")
				// Typically you'd want to implement restart logic here
				return nil, nil

			case rpc.UpdateAgentMethod:
				log.Println("Received update command")
				// Implement your update logic here
				return nil, nil

			default:
				err := fmt.Errorf("unsupported method: %s", req.Method())
				log.Println(err)
				return nil, err
			}
		}),
	)
	if err != nil {
		log.Fatalf("Error creating agent: %v", err)
	}

	err = agent.Connect(context.Background())
	if err != nil {
		log.Fatalf("Error connecting: %v", err)
	}

	log.Printf("Agent connected and ready to handle RPC commands")

	// Create an endpoint for demonstration
	listener, err := agent.Listen(context.Background())
	if err != nil {
		log.Fatalf("Error creating endpoint: %v", err)
	}
	log.Printf("Endpoint created: %s", listener.URL())

	// wait for a stop RPC
	<-stopChan

	// Disconnect agent when done
	log.Println("Shutting down...")
	agent.Disconnect()
}
