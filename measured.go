/*
Package measured wraps a dialer/listener to measure the latency (only for
client connection), throughput and errors of the connection made/accepted.

Throughput is represented as total bytes sent/received between each interval.

ID is the remote address by default.

A list of reporters can be plugged in to send the results to different target.
*/
package measured

import (
	"net"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getlantern/golog"
)

// Stat encapsulates the statistics to report
type Stat interface {
	statType() string
}

// Error encapsulates the error to report
type Error struct {
	ID    string
	Error string
	Phase string
}

// Latency encapsulates the latency data to report
type Latency struct {
	ID      string
	Latency time.Duration
}

// Traffic encapsulates the traffic data to report
type Traffic struct {
	ID       string
	BytesIn  uint64
	BytesOut uint64
}

// LatencyTracker tracks latency in single reporting period
type LatencyTracker struct {
	ID        string
	Min       time.Duration
	Max       time.Duration
	Percent95 time.Duration
	Last      time.Duration
}

// TrafficTracker tracks traffic in single reporting period
type TrafficTracker struct {
	ID           string
	MinIn        uint64
	MaxIn        uint64
	Percent95In  uint64
	LastIn       uint64
	TotalIn      uint64
	MinOut       uint64
	MaxOut       uint64
	Percent95Out uint64
	LastOut      uint64
	TotalOut     uint64
}

const (
	typeError   = "error"
	typeLatency = "latency"
	typeTraffic = "traffic"
)

func (e Error) statType() string   { return typeError }
func (e Latency) statType() string { return typeLatency }
func (e Traffic) statType() string { return typeTraffic }

// Reporter encapsulates different ways to report statistics
type Reporter interface {
	ReportError(map[*Error]int) error
	ReportLatency([]*LatencyTracker) error
	ReportTraffic([]*TrafficTracker) error
}

type tickingReporter struct {
	t *time.Ticker
	r Reporter
}

// Measured is the controller to report statistics
type Measured struct {
	reporters    []Reporter
	chStat       chan Stat
	chStopReport chan interface{}
	chReport     chan Reporter
	chStop       chan interface{}

	errorList   []*Error
	latencyList []*Latency
	trafficList []*Traffic
}

var (
	defaultMeasured *Measured
	log             = golog.LoggerFor("measured")
)

func init() {
	defaultMeasured = New()
}

// DialFunc is the type of function measured can wrap
type DialFunc func(net, addr string) (net.Conn, error)

// Start the default measured instance
func Start(reportInterval time.Duration, reporters ...Reporter) {
	defaultMeasured.Start(reportInterval, reporters...)
}

// Stop the default measured instance
func Stop() {
	defaultMeasured.Stop()
}

// Dialer calls Dialer of the default measured instance
func Dialer(d DialFunc, interval time.Duration) DialFunc {
	return defaultMeasured.Dialer(d, interval)
}

// Listener calls Listener of the default measured instance
func Listener(l net.Listener, interval time.Duration) *MeasuredListener {
	return defaultMeasured.Listener(l, interval)
}

// New creates a new Measured instance
func New() *Measured {
	return &Measured{
		// to avoid blocking when busily reporting stats
		chStat:       make(chan Stat, 10),
		chStopReport: make(chan interface{}),
		chReport:     make(chan Reporter),
		chStop:       make(chan interface{}),
	}
}

// Start runs the measured loop
// Reporting interval should be same for all reporters, as cached data should
// be cleared after each round.

func (m *Measured) Start(reportInterval time.Duration, reporters ...Reporter) {
	go m.run(reportInterval, reporters...)
}

// Stop stops the measured loop
func (m *Measured) Stop() {
	log.Debug("Stopping measured loop...")
	select {
	case m.chStop <- nil:
	default:
		log.Error("Failed to send stop signal")
	}
}

// Dialer wraps a dial function to measure various statistics
func (m *Measured) Dialer(d DialFunc, interval time.Duration) DialFunc {
	return func(net, addr string) (net.Conn, error) {
		c, err := d(net, addr)
		if err != nil {
			m.submitError(addr, err, "dial")
			return nil, err
		}
		return m.newConn(c, interval), nil
	}
}

// Listener wraps a listener to measure various statistics of each connection it accepts
func (m *Measured) Listener(l net.Listener, interval time.Duration) *MeasuredListener {
	return &MeasuredListener{m, l, interval}
}

type MeasuredListener struct {
	m *Measured
	net.Listener
	interval time.Duration
}

// Accept wraps the same function of net.Listener to return a connection
// which measures various statistics
func (l *MeasuredListener) Accept() (c net.Conn, err error) {
	c, err = l.Listener.Accept()
	if err != nil {
		return
	}
	return l.m.newConn(c, l.interval), err
}

func (m *Measured) run(reportInterval time.Duration, reporters ...Reporter) {
	log.Debugf("Measured loop started with %d reporter(s) and interval %v", len(reporters), reportInterval)
	m.reporters = reporters
	t := time.NewTicker(reportInterval)
	for {
		select {
		case s := <-m.chStat:
			switch s.statType() {
			case typeError:
				m.errorList = append(m.errorList, s.(*Error))
			case typeLatency:
				m.latencyList = append(m.latencyList, s.(*Latency))
			case typeTraffic:
				m.trafficList = append(m.trafficList, s.(*Traffic))
			}
		case <-t.C:
			newErrorList := m.errorList
			m.errorList = []*Error{}
			newLatencyList := m.latencyList
			m.latencyList = []*Latency{}
			newTrafficList := m.trafficList
			m.trafficList = []*Traffic{}
			go func() {
				if len(newErrorList) > 0 {
					m.reportError(newErrorList)
				}

				if len(newLatencyList) > 0 {
					m.reportLatency(newLatencyList)
				}

				if len(newTrafficList) > 0 {
					m.reportTraffic(newTrafficList)
				}
			}()
		case <-m.chStop:
			log.Debug("Measured loop stopped")
			return
		}
	}
}

func (m *Measured) reportError(el []*Error) {
	errors := make(map[*Error]int)
	for _, e := range el {
		errors[e] = errors[e] + 1
	}
	for _, r := range m.reporters {
		if err := r.ReportError(errors); err != nil {
			log.Errorf("Failed to report error to %s: %s", reflect.TypeOf(r), err)
		}
	}
}

type latencySorter []*Latency

func (a latencySorter) Len() int           { return len(a) }
func (a latencySorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a latencySorter) Less(i, j int) bool { return a[i].Latency < a[j].Latency }

func (m *Measured) reportLatency(ll []*Latency) {
	lm := make(map[string][]*Latency)
	for _, l := range ll {
		lm[l.ID] = append(lm[l.ID], l)
	}
	trackers := []*LatencyTracker{}
	for k, l := range lm {
		t := LatencyTracker{ID: k}
		t.Last = l[len(l)-1].Latency
		sort.Sort(latencySorter(l))
		t.Min = l[0].Latency
		t.Max = l[len(l)-1].Latency
		p95 := int(float64(len(l)) * 0.95)
		t.Percent95 = l[p95].Latency
		trackers = append(trackers, &t)
	}
	log.Tracef("Reporting %d latency entry", len(trackers))
	for _, r := range m.reporters {
		if err := r.ReportLatency(trackers); err != nil {
			log.Errorf("Failed to report latency data to %s: %s", reflect.TypeOf(r), err)
		}
	}
}

type trafficByBytesIn []*Traffic

func (a trafficByBytesIn) Len() int           { return len(a) }
func (a trafficByBytesIn) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a trafficByBytesIn) Less(i, j int) bool { return a[i].BytesIn < a[j].BytesIn }

type trafficByBytesOut []*Traffic

func (a trafficByBytesOut) Len() int           { return len(a) }
func (a trafficByBytesOut) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a trafficByBytesOut) Less(i, j int) bool { return a[i].BytesOut < a[j].BytesOut }

func (m *Measured) reportTraffic(tl []*Traffic) {
	tm := make(map[string][]*Traffic)
	for _, t := range tl {
		tm[t.ID] = append(tm[t.ID], t)
	}
	trackers := []*TrafficTracker{}
	for k, l := range tm {
		t := TrafficTracker{ID: k}
		t.LastIn = l[len(l)-1].BytesIn
		t.LastOut = l[len(l)-1].BytesOut
		for _, d := range l {
			t.TotalIn = t.TotalIn + d.BytesIn
			t.TotalOut = t.TotalOut + d.BytesOut
		}
		p95 := int(float64(len(l)) * 0.95)

		sort.Sort(trafficByBytesIn(l))
		t.MinIn = l[0].BytesIn
		t.MaxIn = l[len(l)-1].BytesIn
		t.Percent95In = l[p95].BytesIn

		sort.Sort(trafficByBytesOut(l))
		t.MinOut = l[0].BytesOut
		t.MaxOut = l[len(l)-1].BytesOut
		t.Percent95Out = l[p95].BytesOut
		trackers = append(trackers, &t)
	}
	log.Tracef("Reporting %d traffic entry", len(trackers))
	for _, r := range m.reporters {
		if err := r.ReportTraffic(trackers); err != nil {
			log.Errorf("Failed to report traffic data to %s: %s", reflect.TypeOf(r), err)
		}
	}
}

// Conn wraps any net.Conn to add statistics
type Conn struct {
	net.Conn
	// arbitrary string to identify this connection, defaults to remote address
	ID string
	// total bytes read from this connection
	BytesIn uint64
	// total bytes wrote to this connection
	BytesOut uint64
	// a channel to stop measure and report statistics
	chStop chan interface{}
	m      *Measured
}

func (m *Measured) newConn(c net.Conn, interval time.Duration) net.Conn {
	ra := c.RemoteAddr()
	if ra == nil {
		panic("nil remote address is not allowed")
	}
	mc := &Conn{Conn: c, ID: ra.String(), chStop: make(chan interface{}), m: m}
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case _ = <-ticker.C:
				mc.submitTraffic()
			case _ = <-mc.chStop:
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
		mc.submitError(err, "read")
	}
	atomic.AddUint64(&mc.BytesIn, uint64(n))
	return
}

// Write() implements the function from net.Conn
func (mc *Conn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	if err != nil {
		mc.submitError(err, "write")
	}
	atomic.AddUint64(&mc.BytesOut, uint64(n))
	return
}

// Close implements the function from net.Conn
func (mc *Conn) Close() (err error) {
	err = mc.Conn.Close()
	if err != nil {
		mc.submitError(err, "close")
	}
	mc.submitTraffic()
	mc.chStop <- nil
	return
}

func (mc *Conn) submitError(err error, phase string) {
	mc.m.submitError(mc.ID, err, phase)
}

func (mc *Conn) submitTraffic() {
	mc.m.submitTraffic(mc.ID,
		atomic.SwapUint64(&mc.BytesIn, 0),
		atomic.SwapUint64(&mc.BytesOut, 0))
}

func (m *Measured) submitError(connID string, err error, phase string) {
	splitted := strings.Split(err.Error(), ":")
	lastIndex := len(splitted) - 1
	if lastIndex < 0 {
		lastIndex = 0
	}
	e := strings.Trim(splitted[lastIndex], " ")
	er := &Error{
		ID:    connID,
		Error: e,
		Phase: phase,
	}
	log.Tracef("Submiting error %+v", er)
	select {
	case m.chStat <- er:
	default:
		log.Error("Failed to submit error, channel busy")
	}
}

func (m *Measured) submitTraffic(connID string, BytesIn uint64, BytesOut uint64) {
	t := &Traffic{
		ID:       connID,
		BytesIn:  BytesIn,
		BytesOut: BytesOut,
	}
	log.Tracef("Submiting traffic %+v", t)
	select {
	case m.chStat <- t:
	default:
		log.Error("Failed to submit traffic, channel busy")
	}
}
