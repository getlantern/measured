/*
Package measured wraps a dialer to measure the delay, throughput and errors of the connection made.
A list of reporters can be plugged in to distribute the results to different target.
*/
package measured

import (
	"net"
	"strings"
	"sync/atomic"
	"time"

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
	reporters   atomic.Value
	defaultTags atomic.Value
	running     uint32
	log         = golog.LoggerFor("measured")
	// to avoremoteAddr blocking when busily reporting stats
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
func Dialer(d DialFunc, remoteAddr string, interval time.Duration) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			reportError(remoteAddr, err, "dial")
		}
		return newMeasuredConn(c, remoteAddr, interval), err
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

// Accept wraps the function of an net.Listener to return an connection
// which measures various statistics
func (l *measuredListener) Accept() (c net.Conn, err error) {
	c, err = l.Listener.Accept()
	if err != nil {
		return
	}
	addr := ""
	if ra := c.RemoteAddr(); ra != nil {
		addr = ra.String()
	}
	return newMeasuredConn(c, addr, l.interval), err
}

func run() {
	log.Debug("Measured loop started")
	atomic.StoreUint32(&running, 1)
	for {
		select {
		case s := <-chStats:
			defaults := defaultTags.Load().(map[string]string)
			for _, r := range reporters.Load().([]Reporter) {
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

type measuredConn struct {
	net.Conn
	remoteAddr string
	bytesIn    uint64
	bytesOut   uint64
	chStop     chan interface{}
}

func newMeasuredConn(c net.Conn, remoteAddr string, interval time.Duration) net.Conn {
	mc := &measuredConn{Conn: c, remoteAddr: remoteAddr, chStop: make(chan interface{})}
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
func (mc *measuredConn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	if err != nil {
		reportError(mc.remoteAddr, err, "read")
	}
	atomic.AddUint64(&mc.bytesIn, uint64(n))
	return
}

// Write() implements the function from net.Conn
func (mc *measuredConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	if err != nil {
		reportError(mc.remoteAddr, err, "write")
	}
	atomic.AddUint64(&mc.bytesOut, uint64(n))
	return
}

// Close() implements the function from net.Conn
func (mc *measuredConn) Close() (err error) {
	err = mc.Conn.Close()
	if err != nil {
		reportError(mc.remoteAddr, err, "close")
	}
	mc.reportStats()
	mc.chStop <- nil
	return
}

func (mc *measuredConn) reportStats() {
	reportStats(mc.remoteAddr,
		atomic.LoadUint64(&mc.bytesIn),
		atomic.LoadUint64(&mc.bytesOut))
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
		Tags: map[string]string{
			"remoteAddr": remoteAddr,
			"error":      e,
			"phase":      phase,
		},
		Fields: map[string]interface{}{"value": 1},
	}:
	default:
		log.Error("Failed to send stats to reporters")
	}
}

func reportStats(remoteAddr string, bytesIn uint64, bytesOut uint64) {
	select {
	case chStats <- &Stats{
		Type: "stats",
		Tags: map[string]string{
			"remoteAddr": remoteAddr,
		},
		Fields: map[string]interface{}{
			"bytesIn":  bytesIn,
			"bytesOut": bytesOut,
		},
	}:
	default:
		log.Error("Failed to send stats to reporters")
	}
}
