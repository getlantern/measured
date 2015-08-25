/*
Package measured wraps a dialer to measure the delay, throughput and errors of the connection made.
A list of reporters can be plugged in to distribute the results to different target.
*/
package measured

import (
	"net"
	"strings"

	"github.com/getlantern/golog"
)

// Stats encapsulates the statistics to report
type Stats struct {
	Country      string
	Server       string
	BytesRead    uint64
	BytesWritten uint64
	Errors       map[string]int
}

// Reporter encapsulates different ways to report statistics
type Reporter interface {
	Submit(*Stats) error
}

var (
	reporters []Reporter
	log       = golog.LoggerFor("measured")
	chStats   = make(chan *Stats)
	chStop    = make(chan interface{})
)

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

// Start runs the reporting process
func Start() {
	go run()
}

// Stop runs the reporting process
func Stop() {
	log.Debug("Stopping measured loop...")
	select {
	case chStop <- nil:
	default:
	}
}

// Dialer wraps a dial function to measure various metrics
func Dialer(d DialFunc, via string) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			reportError(via, err)
		}
		return measuredConn{c, via}, err
	}
}

func run() {
	log.Debug("Measured loop started")
	for {
		select {
		case s := <-chStats:
			for _, r := range reporters {
				if err := r.Submit(s); err != nil {
					log.Errorf("report error to influxdb failed: %s", err)
				} else {
					log.Tracef("submitted error to influxdb: %v", s)
				}
			}
		case <-chStop:
			return
		}
	}
}

func reportError(addr string, err error) {
	splitted := strings.Split(err.Error(), ":")
	e := strings.Trim(splitted[len(splitted)-1], " ")
	select {
	case chStats <- &Stats{
		Server: addr,
		Errors: map[string]int{e: 1},
	}:
	default:
	}
}

type measuredConn struct {
	net.Conn
	addr string
}

// Read() implements the function from net.Conn
func (mc measuredConn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	if err != nil {
		reportError(mc.addr, err)
	}
	return
}

// Write() implements the function from net.Conn
func (mc measuredConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	if err != nil {
		reportError(mc.addr, err)
	}
	return
}

// Close() implements the function from net.Conn
func (mc measuredConn) Close() (err error) {
	err = mc.Conn.Close()
	if err != nil {
		reportError(mc.addr, err)
	}
	return
}
