/*
Package measured wraps a dialer to measure the delay, throughput and errors of the connection made.
A list of reporters can be plugged in to distribute the results to different target.
*/
package measured

import (
	"net"
	"strings"
	//	"time"

	"github.com/getlantern/golog"
)

// Stats encapsulates the statistics to report
type Stats struct {
	Instance     string
	Server       string
	BytesRead    uint64
	BytesWritten uint64
	Errors       map[string]int
}

// Reporter encapsulates different ways to report statistics
type Reporter interface {
	Submit(Stats) error
}

var reporters []Reporter

var log = golog.LoggerFor("measured")

// DialFunc is the type of function measured can wrap
type DialFunc func(net, addr string) (net.Conn, error)

// Reset resets the measured package
func Reset() {
	reporters = []Reporter{}
}

// AddReporter add a new way to report statistics
func AddReporter(r Reporter) {
	reporters = append(reporters, r)
}

// Dialer wraps a dial function to measure various metrics
func Dialer(d DialFunc) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			reportError(addr, err)
		}
		return measuredConn{c, addr, new(counter)}, err
	}
}

func reportError(addr string, err error) {
	splitted := strings.Split(err.Error(), ":")
	e := strings.Trim(splitted[len(splitted)-1], " ")
	for _, r := range reporters {
		if err = r.Submit(Stats{
			Server: addr,
			Errors: map[string]int{e: 1},
		}); err != nil {
			log.Errorf("Fail to report error of %s to influxdb: %s", addr, err)
		} else {
			log.Tracef("Submitted error of %s to influxdb: %s", addr, e)
		}
	}
}

type measuredConn struct {
	net.Conn
	addr string
	c    *counter
}

// Read() implements the function from net.Conn
func (mc measuredConn) Read(b []byte) (n int, err error) {
	//start := time.Now()
	n, err = mc.Conn.Read(b)
	if err != nil {
		reportError(mc.addr, err)
	}
	//mc.c.OnRead(n, err, time.Now()-start)
	return
}

// Write() implements the function from net.Conn
func (mc measuredConn) Write(b []byte) (n int, err error) {
	//start := time.Now()
	n, err = mc.Conn.Write(b)
	if err != nil {
		reportError(mc.addr, err)
	}
	//mc.c.OnWrite(n, err, time.Now()-start)
	return
}

// Close() implements the function from net.Conn
func (mc measuredConn) Close() (err error) {
	//start := time.Now()
	err = mc.Conn.Close()
	if err != nil {
		reportError(mc.addr, err)
	}
	//mc.c.OnClose(err, time.Now()-start)
	return
}

type counter struct {
}
