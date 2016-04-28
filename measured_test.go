package measured

import (
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getlantern/testify/assert"
)

func TestReportError(t *testing.T) {
	md, nr := startWithMockReporter()
	defer md.Stop()
	d := md.Dialer(net.Dial, 10*time.Second)
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(100 * time.Millisecond)
	nr.Lock()
	defer nr.Unlock()
	if assert.Equal(t, 2, len(nr.error), "should report errors") {
		assert.Equal(t, 1, nr.error[Error{"localhost:9999", "connection refused", "dial"}])
		assert.Equal(t, 1, nr.error[Error{"localhost:9998", "connection refused", "dial"}])
	}
}

func TestStopAndRestart(t *testing.T) {
	md, nr := startWithMockReporter()
	md.Stop()
	d := md.Dialer(net.Dial, 10*time.Second)
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(100 * time.Millisecond)
	nr.Lock()
	assert.Equal(t, 0, len(nr.error), "stopped measured should not submit any metrics")
	nr.Unlock()
	md.Start(50*time.Millisecond, nr)
	defer md.Stop()
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(100 * time.Millisecond)
	nr.Lock()
	defer nr.Unlock()
	if assert.Equal(t, 2, len(nr.error), "should report again if restarted") {
		assert.Equal(t, 1, nr.error[Error{"localhost:9999", "connection refused", "dial"}])
		assert.Equal(t, 1, nr.error[Error{"localhost:9998", "connection refused", "dial"}])
	}
}

func TestReportStats(t *testing.T) {
	md, nr := startWithMockReporter()
	defer md.Stop()
	var bytesIn, bytesOut uint64
	var RemoteAddr string

	// start server with byte counting
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if !assert.NoError(t, err, "Listen should not fail") {
		return
	}

	// large enough interval so it will only report stats in Close()
	ml := md.Listener(l, 10*time.Second)
	s := http.Server{
		Handler: http.NotFoundHandler(),
		ConnState: func(c net.Conn, s http.ConnState) {
			if s == http.StateIdle {
				RemoteAddr = c.RemoteAddr().String()
				mc := c.(*Conn)
				atomic.StoreUint64(&bytesIn, mc.BytesIn)
				atomic.StoreUint64(&bytesOut, mc.BytesOut)
			}
		},
	}
	go func() { _ = s.Serve(ml) }()

	time.Sleep(100 * time.Millisecond)
	// start client with byte counting
	c := http.Client{
		Transport: &http.Transport{
			// carefully chosen interval to report another once before Close()
			Dial: md.Dialer(net.Dial, 160*time.Millisecond),
		},
	}
	req, _ := http.NewRequest("GET", "http://"+l.Addr().String(), nil)
	resp, _ := c.Do(req)
	assert.Equal(t, 404, resp.StatusCode)
	assert.True(t, atomic.LoadUint64(&bytesIn) > 0, "should count bytesIn")
	assert.True(t, atomic.LoadUint64(&bytesOut) > 0, "should count bytesOut")

	// make sure client will report another once
	time.Sleep(200 * time.Millisecond)
	// Close without reading from body, to force server to close connection
	_ = resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	// verify both client and server stats
	nr.Lock()
	defer nr.Unlock()
	if assert.Equal(t, 3, len(nr.traffic)) {
		t.Logf("Traffic entries: %+v", nr.traffic)
		e := nr.traffic[0]
		assert.Equal(t, l.Addr().String(), e.ID, "client stats should report server as remote addr")
		assert.Equal(t, bytesIn, e.MinOut, "client stats should report same byte count as server")
		assert.Equal(t, bytesOut, e.MinIn, "client stats should report same byte count as server")
		assert.Equal(t, bytesIn, e.LastOut, "client stats should report same byte count as server")
		assert.Equal(t, bytesOut, e.LastIn, "client stats should report same byte count as server")
		assert.Equal(t, bytesIn, e.TotalOut, "client stats should report same byte count as server")
		assert.Equal(t, bytesOut, e.TotalIn, "client stats should report same byte count as server")

		// entries reported in a batch are in random order
		for _, e = range nr.traffic[1:] {
			if e.ID == l.Addr().String() {
				assert.Equal(t, uint64(0), e.MinOut, "client stats should only report increased byte count")
				assert.Equal(t, uint64(0), e.MinIn, "client stats should only report increased byte count")
				assert.Equal(t, uint64(0), e.LastOut, "client stats should only report increased byte count")
				assert.Equal(t, uint64(0), e.LastIn, "client stats should only report increased byte count")
				assert.Equal(t, uint64(0), e.TotalOut, "client stats should only report increased byte count")
				assert.Equal(t, uint64(0), e.TotalIn, "client stats should only report increased byte count")

			} else {
				assert.Equal(t, RemoteAddr, e.ID, "should report server stats with client addr as ID")
				assert.Equal(t, bytesIn, e.TotalIn, "should report server stats with bytes in")
				assert.Equal(t, bytesOut, e.TotalOut, "should report server stats with bytes out")
				assert.Equal(t, bytesIn, e.LastIn, "should report server stats with bytes in")
				assert.Equal(t, bytesOut, e.LastOut, "should report server stats with bytes out")
				assert.Equal(t, bytesIn, e.MinIn, "should report server stats with bytes in")
				assert.Equal(t, bytesOut, e.MinOut, "should report server stats with bytes out")
			}
		}
	}
}

func startWithMockReporter() (*Measured, *mockReporter) {
	nr := mockReporter{
		error: make(map[Error]int),
	}
	md := New()
	md.Start(50*time.Millisecond, &nr)
	// To make sure it really started
	runtime.Gosched()
	return md, &nr
}

type mockReporter struct {
	sync.Mutex
	error   map[Error]int
	latency []*LatencyTracker
	traffic []*TrafficTracker
}

func (nr *mockReporter) ReportError(e map[*Error]int) error {
	nr.Lock()
	defer nr.Unlock()
	for k, v := range e {
		nr.error[*k] = v
	}
	return nil
}

func (nr *mockReporter) ReportLatency(l []*LatencyTracker) error {
	nr.Lock()
	defer nr.Unlock()
	nr.latency = append(nr.latency, l...)
	return nil
}

func (nr *mockReporter) ReportTraffic(t []*TrafficTracker) error {
	nr.Lock()
	defer nr.Unlock()
	nr.traffic = append(nr.traffic, t...)
	return nil
}
