package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// localtunnel.me exposes a local port to a public https URL. The protocol is
// simple: GET https://localtunnel.me/?new reserves a subdomain and returns a
// TCP port to connect relay agents to; each agent connection is bridged to the
// local server, and localtunnel multiplexes incoming requests across the pool.
const tunnelHost = "localtunnel.me"

type tunnelInfo struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Port         int    `json:"port"`
	MaxConnCount int    `json:"max_conn_count"`
}

// startLocalTunnel reserves a public URL and spawns the relay pool. It blocks
// only for the initial reservation request; on success the returned URL is
// live (workers reconnect on their own for the life of the process).
func startLocalTunnel(localPort int) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://%s/?new", tunnelHost))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %s", tunnelHost, resp.Status)
	}
	var info tunnelInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.URL == "" || info.Port == 0 {
		return "", fmt.Errorf("unexpected response from %s", tunnelHost)
	}
	conns := info.MaxConnCount
	if conns <= 0 {
		conns = 10
	}
	for i := 0; i < conns; i++ {
		go tunnelWorker(info.Port, localPort)
	}
	return info.URL, nil
}

// tunnelWorker keeps one relay connection alive: dial localtunnel, dial the
// local server, splice them, and reconnect when the pairing ends.
func tunnelWorker(remotePort, localPort int) {
	remoteAddr := net.JoinHostPort(tunnelHost, strconv.Itoa(remotePort))
	localAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))
	for {
		remote, err := net.DialTimeout("tcp", remoteAddr, 10*time.Second)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		local, err := net.Dial("tcp", localAddr)
		if err != nil {
			remote.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		splice(remote, local)
	}
}

// splice copies bytes both ways until either side closes, then closes both.
func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
	a.Close()
	b.Close()
}
