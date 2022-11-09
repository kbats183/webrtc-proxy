package main

import (
	"fmt"
	"github.com/pion/webrtc/v3"
	"log"
	"sync"
)

type proxyConnection struct {
	key         string
	state       webrtc.PeerConnectionState
	error       error
	connection  *webrtc.PeerConnection
	connections *proxyConnections
}

func (c *proxyConnection) HandleError(err error) {
	c.error = err
	log.Printf("Connection %s %s", c.key, err.Error())
}

func (c *proxyConnection) Close() {
	_ = c.connection.Close()
	c.connections.CloseConnection(c.key)
}

func (c *proxyConnection) ChangeState(state webrtc.PeerConnectionState) {
	c.state = state
	log.Printf("Connection %s new state %s", c.key, state.String())
}

type proxyConnections struct {
	connections []proxyConnection
	mutex       sync.Mutex
}

func (t *proxyConnections) NewConnection(connection *webrtc.PeerConnection, source string) *proxyConnection {
	key := randConnectionKey(10)
	proxyConnection := proxyConnection{connection: connection, key: key, state: connection.ConnectionState()}
	t.mutex.Lock()
	t.connections = append(t.connections, proxyConnection)
	t.mutex.Unlock()
	proxyConnection.connections = t
	log.Printf("New connection %s (%s)\n", key, source)
	return &proxyConnection
}

func (t *proxyConnections) CloseConnection(key string) {
	t.mutex.Lock()
	oldConnections := t.connections
	t.connections = []proxyConnection{}
	for _, conn := range oldConnections {
		if conn.key != key {
			t.connections = append(t.connections, conn)
		}
	}
	t.mutex.Unlock()
}

func (t *proxyConnections) GetStatus() []string {
	var resp []string
	t.mutex.Lock()
	for _, conn := range t.connections {
		l := fmt.Sprintf("%s: %s", conn.key, conn.state.String())
		if conn.error != nil {
			l += ": " + conn.error.Error()
		}
		resp = append(resp, l)
	}
	t.mutex.Unlock()

	return resp
}
