/*
Package measured wraps a dialer/listener to measure the delay, throughput and
errors of the connection made/accepted.

Throughput is represented by total bytes sent/received between each interval.

A list of reporters can be plugged in to distribute the results to different
target.
*/
package measured

import (
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getlantern/golog"
)

type Tags map[string]string

func (t *Tags) Compare(rhs *Tags) int {
	return 0
}

type Fields map[string]interface{}

// Stats encapsulates the statistics to report
type Stats struct {
	Type   string
	Tags   Tags
	Fields Fields
}

// Reporter encapsulates different ways to report statistics
type Reporter interface {
	Submit(*Stats) error
}

var (
	reporters   atomic.Value
	defaultTags atomic.Value
	log         = golog.LoggerFor("measured")
	// to avoid blocking when busily reporting stats
	chStats = make(chan *Stats, 10)
	chStop  = make(chan interface{})
)

func init() {
	Reset()
}

// DialFunc is the type of function measured can wrap
type DialFunc func(net, addr string) (net.Conn, error)

// Reset resets the measured package
func Reset() {
	defaultTags.Store(map[string]string{})
	reporters.Store([]Reporter{})
}

// AddReporter add a new way to report statistics
func AddReporter(r Reporter) {
	reporters.Store(append(reporters.Load().([]Reporter), r))
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
	log.Debug("Stopping measured loop...")
	select {
	case chStop <- nil:
	default:
		log.Error("Failed to send stop signal")
	}
}

// Dialer wraps a dial function to measure various statistics
func Dialer(d DialFunc, interval time.Duration) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			reportError(addr, err, "dial")
		}
		return newConn(c, interval), err
	}
}

// Dialer wraps a dial function to measure various statistics
func Listener(l net.Listener, interval time.Duration) net.Listener {
	return &measuredListener{l, interval}
}

type measuredListener struct {
	net.Listener
	interval time.Duration
}

// Accept wraps the same function of net.Listener to return a connection
// which measures various statistics
func (l *measuredListener) Accept() (c net.Conn, err error) {
	c, err = l.Listener.Accept()
	if err != nil {
		return
	}
	return newConn(c, l.interval), err
}

func run() {
	log.Debug("Measured loop started")
	for {
		select {
		case s := <-chStats:
			defaults := defaultTags.Load().(map[string]string)
			for k, v := range defaults {
				s.Tags[k] = v
			}
			for _, r := range reporters.Load().([]Reporter) {
				if err := r.Submit(s); err != nil {
					log.Errorf("Failed to report error to influxdb: %s", err)
				} else {
					log.Tracef("Submitted error to influxdb: %v", s)
				}
			}
		case <-chStop:
			log.Debug("Measured loop stopped")
			return
		}
	}
}

// Conn wraps any net.Conn to add statistics
type Conn struct {
	net.Conn
	// total bytes read from this connection
	BytesIn uint64
	// total bytes wrote to this connection
	BytesOut uint64
	// extra tags related to this connection, will submit to reporters eventually
	// not protected
	ExtraTags map[string]string
	chStop    chan interface{}
}

func newConn(c net.Conn, interval time.Duration) net.Conn {
	mc := &Conn{Conn: c, ExtraTags: make(map[string]string), chStop: make(chan interface{})}
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case _ = <-ticker.C:
				mc.reportStats()
			case _ = <-chStop:
				ticker.Stop()
				return
			}
		}
	}()
	return mc
}

// Read() implements the function from net.Conn
func (mc *Conn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	if err != nil {

		mc.reportError(err, "read")
	}
	atomic.AddUint64(&mc.BytesIn, uint64(n))
	return
}

// Write() implements the function from net.Conn
func (mc *Conn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	if err != nil {
		mc.reportError(err, "write")
	}
	atomic.AddUint64(&mc.BytesOut, uint64(n))
	return
}

// Close() implements the function from net.Conn
func (mc *Conn) Close() (err error) {
	err = mc.Conn.Close()
	if err != nil {
		mc.reportError(err, "close")
	}
	mc.reportStats()
	mc.chStop <- nil
	return
}

func (mc *Conn) reportError(err error, phase string) {
	ra := mc.Conn.RemoteAddr()
	if ra == nil {
		log.Error("Remote address is nil, not report error")
		return
	}
	reportError(ra.String(), err, phase)
}

func (mc *Conn) reportStats() {
	ra := mc.Conn.RemoteAddr()
	if ra == nil {
		log.Error("Remote address is nil, not report stats")
		return
	}
	reportStats(ra.String(),
		atomic.SwapUint64(&mc.BytesIn, 0),
		atomic.SwapUint64(&mc.BytesOut, 0))
}

func reportError(remoteAddr string, err error, phase string) {
	splitted := strings.Split(err.Error(), ":")
	lastIndex := len(splitted) - 1
	if lastIndex < 0 {
		lastIndex = 0
	}
	e := strings.Trim(splitted[lastIndex], " ")
	select {
	case chStats <- &Stats{
		Type: "errors",
		Tags: Tags{
			"remoteAddr": remoteAddr,
			"error":      e,
			"phase":      phase,
		},
		Fields: Fields{"value": 1},
	}:
	default:
		log.Error("Failed to send stats to reporters")
	}
}

func reportStats(remoteAddr string, BytesIn uint64, BytesOut uint64) {
	select {
	case chStats <- &Stats{
		Type: "stats",
		Tags: Tags{
			"remoteAddr": remoteAddr,
		},
		Fields: Fields{
			"bytesIn":  BytesIn,
			"bytesOut": BytesOut,
		},
	}:
	default:
		log.Error("Failed to send stats to reporters")
	}
}
