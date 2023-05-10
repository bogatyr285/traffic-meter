package main

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/things-go/go-socks5"
)

func main() {
	mainCtx := context.Background()
	// Create a SOCKS5 server
	server := socks5.NewServer(
		socks5.WithLogger(socks5.NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)

	listener, err := net.Listen("tcp", ":10800")
	if err != nil {
		panic(err)
	}
	w := NewTrafficMeter(listener)
	w.SetUserLimit(500)
	go w.RunLogging(mainCtx)

	if err := server.Serve(w); err != nil {
		log.Fatal("serving err ", err)
	}
}
