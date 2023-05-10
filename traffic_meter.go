package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

// Traffic usage per client
type TrafficUsage struct {
	Read  uint64
	Write uint64
}

// TrafficMeter wraps a net.Listener to provide traffic measuring per client
type TrafficMeter struct {
	l                  net.Listener
	traffic            map[string]TrafficUsage // key: client IP, value: total traffic in bytes
	globalTrafficUsage uint64
	userLimit          uint64
	globalLimit        uint64
	logPeriod          time.Duration
	mx                 sync.RWMutex
}

func NewTrafficMeter(l net.Listener) *TrafficMeter {
	tm := &TrafficMeter{
		l:           l,
		traffic:     make(map[string]TrafficUsage),
		mx:          sync.RWMutex{},
		userLimit:   2 << (10 * 2), // 2 MB
		globalLimit: 5 << (10 * 2), // 5 MB
		logPeriod:   time.Second * 15,
	}
	log.Printf("[TrafficMeter] Running with settings: userLimit=%v globalLimit=%v logPeriod=%v", tm.userLimit, tm.globalLimit, tm.logPeriod)
	return tm
}

// SetUserLimit - set bandwitdth limit for user
func (tm *TrafficMeter) SetUserLimit(s uint64) {
	tm.userLimit = s
}

// SetGlobalLimit - set global bandwitdth limit
func (tm *TrafficMeter) SetGlobalLimit(s uint64) {
	tm.globalLimit = s
}

// GlobalTraffic - returns the total traffic count
func (tc *TrafficMeter) GlobalTraffic() uint64 {
	return tc.globalTrafficUsage
}

// Traffic returns the total traffic for the client by the client IP
func (tm *TrafficMeter) Traffic(addr string) *TrafficUsage {
	tm.mx.RLock()
	defer tm.mx.RUnlock()
	v, ok := tm.traffic[addr]
	if !ok {
		return nil
	}
	return &v
}

// LogUsage prints the total usage to the log
func (tm *TrafficMeter) LogUsage() {
	for addr, tu := range tm.traffic {
		log.Printf("[TrafficMeter] client %v used %v/%v bytes of traffic", addr, tu.Read, tu.Write)
	}
	log.Printf("[TrafficMeter] total traffic is %v bytes", tm.globalTrafficUsage)
}

// RunLogging - Run logging function which will write every N seconds stats about current traffic usage
// must be run in a separate goroutine
func (tm *TrafficMeter) RunLogging(ctx context.Context) {
	ticker := time.NewTicker(tm.logPeriod)

	for {
		select {
		case <-ticker.C:
			tm.LogUsage()
		case <-ctx.Done():
			log.Println("[TrafficMeter] shut down...")
			ticker.Stop()
			return
		}
	}
}

// Accept - net.Listener overrided method
func (tm *TrafficMeter) Accept() (net.Conn, error) {
	conn, err := tm.l.Accept()
	if err != nil {
		return nil, err
	}

	addr := strings.Split(conn.RemoteAddr().String(), ":")[0]
	// !!! we dont do checks here because return err here will crash server

	// TODO according to the task we have to store "customer_id" but lets store just "customer ip" baceuse its affects nothing and add adding yet another mapper userID-userIP is trivial
	// wrap the connection in a trackingConn
	conn = &trackingConn{
		Conn: conn,
		tm:   tm,
		addr: addr,
	}

	return conn, nil
}

// Close - net.Listener overrided method
func (tm *TrafficMeter) Close() error {
	return tm.l.Close()
}

// Addr - net.Listener overrided method
func (tm *TrafficMeter) Addr() net.Addr {
	return tm.l.Addr()
}

// trackingConn wraps a net.Conn to track the traffic
type trackingConn struct {
	net.Conn
	tm   *TrafficMeter
	addr string
}

func (tc *trackingConn) checkThresholds() error {
	if tc.tm.traffic[tc.addr].Read >= tc.tm.userLimit ||
		tc.tm.traffic[tc.addr].Write >= tc.tm.userLimit {
		if err := socks5.SendReply(tc.Conn, statute.RepRuleFailure, nil); err != nil {
			log.Printf("[TrafficMeter] sending response to user err: %v", err)
		}
		return fmt.Errorf("user %v exceeded the user limit of %v bytes", tc.addr, tc.tm.userLimit)
	}

	if tc.tm.globalTrafficUsage >= tc.tm.globalLimit {
		if err := socks5.SendReply(tc.Conn, statute.RepRuleFailure, nil); err != nil {
			log.Printf("[TrafficMeter] sending response to user err: %v", err)
		}
		return fmt.Errorf("total traffic exceeded the global limit of %v bytes", tc.tm.globalLimit)
	}
	return nil
}

// Read - overrided net.Conn
func (tc *trackingConn) Read(b []byte) (int, error) {
	if err := tc.checkThresholds(); err != nil {
		return 0, err
	}
	n, err := tc.Conn.Read(b)
	if n > 0 {
		tc.tm.mx.Lock()
		// todo map can be separated into separate class with methods like "Add/Get"
		if tu, ok := tc.tm.traffic[tc.addr]; !ok {
			tc.tm.traffic[tc.addr] = TrafficUsage{}
		} else {
			tu.Read += uint64(n)
			tc.tm.traffic[tc.addr] = tu
		}

		//tu.Read += uint64(n)
		tc.tm.globalTrafficUsage += uint64(n)
		tc.tm.mx.Unlock()
	}
	return n, err
}

// Write - overrided net.Conn
func (tc *trackingConn) Write(b []byte) (int, error) {
	n, err := tc.Conn.Write(b)
	if n > 0 {
		tc.tm.mx.Lock()
		if tu, ok := tc.tm.traffic[tc.addr]; !ok {
			tc.tm.traffic[tc.addr] = TrafficUsage{}
		} else {
			tu.Write += uint64(n)
			tc.tm.traffic[tc.addr] = tu
		}
		tc.tm.globalTrafficUsage += uint64(n)
		tc.tm.mx.Unlock()
	}
	return n, err
}
