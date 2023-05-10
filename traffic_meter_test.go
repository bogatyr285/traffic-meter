package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/things-go/go-socks5"
	"golang.org/x/net/proxy"
)

func TestTrafficMeter(t *testing.T) {
	// Create a local listener
	addr := "127.0.0.1:53000"
	targetURL := "https://example.com"
	l, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	tm := NewTrafficMeter(l)
	tm.SetUserLimit(7000)

	// Create a SOCKS5 server
	srv := socks5.NewServer(
		socks5.WithLogger(socks5.NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)

	// Start listening
	go func() {
		err := srv.Serve(tm)
		require.NoError(t, err)
	}()
	time.Sleep(10 * time.Millisecond)

	dialer, err := proxy.SOCKS5("tcp", addr, &proxy.Auth{}, nil)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: dialer.Dial,
		},
	}

	r, err := client.Get(targetURL)
	require.NoError(t, err)
	defer r.Body.Close()
	// test global traffic calc & user traffic
	gt := tm.GlobalTraffic()
	tu := tm.Traffic("127.0.0.1")
	log.Printf("#1 Global traffic: %v. user: %+v", gt, tu)
	require.NotNil(t, tu)
	require.Equal(t, gt, tu.Read+tu.Write+4) // 4 - header

	// test user exeeding
	_, err = client.Get(targetURL)
	require.Error(t, err, "expect to get exeeding error")

	gt = tm.GlobalTraffic()
	tu = tm.Traffic("127.0.0.1")
	require.NotNil(t, tu)
	log.Printf("#2 Global traffic: %v. user: %+v", gt, tu)
	require.Equal(t, gt, tu.Read+tu.Write+4) // 4 - header
}
