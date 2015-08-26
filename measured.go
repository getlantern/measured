/*
Package measured wraps a dialer to measure the delay, throughput and errors of the connection made.
A list of reporters can be plugged in to distribute the results to different target.
*/
package measured

import (
	"net"
	"strings"
	"sync/atomic"

	"github.com/getlantern/golog"
)

// Stats encapsulates the statistics to report
type Stats struct {
	Type   string
	Tags   map[string]string
	Fields map[string]interface{}
}

// Reporter encapsulates different ways to report statistics
type Reporter interface {
	Submit(*Stats) error
}

var (
	reporters   []Reporter
	defaultTags atomic.Value
	running     uint32
	log         = golog.LoggerFor("measured")
	chStats     = make(chan *Stats)
	chStop      = make(chan interface{})
)

func init() {
	defaultTags.Store(map[string]string{})
}

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

// SetDefaults set a few default tags sending every time
func SetDefaults(defaults map[string]string) {
	defaultTags.Store(defaults)
}

// Start runs the measured loop
func Start() {
	go run()
}

// Stop stops the measured loop
func Stop() {
	if atomic.LoadUint32(&running) == 0 {
		return
	}
	log.Debug("Stopping measured loop...")
	select {
	case chStop <- nil:
	default:
		log.Error("Failed to send stop signal")
	}
}

// Dialer wraps a dial function to measure various statistics
func Dialer(d DialFunc, via string) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			reportError(via, err, "dial")
		}
		return measuredConn{c, via}, err
	}
}

func run() {
	log.Debug("Measured loop started")
	atomic.StoreUint32(&running, 1)
	for {
		select {
		case s := <-chStats:
			defaults := defaultTags.Load().(map[string]string)
			for _, r := range reporters {
				for k, v := range defaults {
					s.Tags[k] = v
				}
				if err := r.Submit(s); err != nil {
					log.Errorf("Failed to report error to influxdb: %s", err)
				} else {
					log.Tracef("Submitted error to influxdb: %v", s)
				}
			}
		case <-chStop:
			log.Debug("Measured loop stopped")
			atomic.StoreUint32(&running, 0)
			return
		}
	}
}

func reportError(addr string, err error, phase string) {
	splitted := strings.Split(err.Error(), ":")
	lastIndex := len(splitted) - 1
	if lastIndex < 0 {
		lastIndex = 0
	}
	e := strings.Trim(splitted[lastIndex], " ")
	select {
	case chStats <- &Stats{
		Type: "errors",
		Tags: map[string]string{
			"server": addr,
			"error":  e,
			"phase":  phase,
		},
		Fields: map[string]interface{}{"value": 1},
	}:
	default:
		log.Error("Failed to send stats to reporters")
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
		reportError(mc.addr, err, "read")
	}
	return
}

// Write() implements the function from net.Conn
func (mc measuredConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	if err != nil {
		reportError(mc.addr, err, "write")
	}
	return
}

// Close() implements the function from net.Conn
func (mc measuredConn) Close() (err error) {
	err = mc.Conn.Close()
	if err != nil {
		reportError(mc.addr, err, "close")
	}
	return
}
