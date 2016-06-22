package measured

import (
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReportStats(t *testing.T) {
	md, nr := startWithMockReporter()
	defer md.Stop()
	var remoteAddr atomic.Value

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
			if s == http.StateClosed {
				remoteAddr.Store(c.RemoteAddr().String())
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

	// Close without reading from body, to force server to close connection
	_ = resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	nr.Lock()
	defer nr.Unlock()
	t.Logf("Traffic entries: %+v", nr.traffic)
	if assert.Equal(t, 2, len(nr.traffic)) {
		ct := nr.traffic[l.Addr().String()]
		st := nr.traffic[remoteAddr.Load().(string)]

		if assert.NotNil(t, ct) {
			assert.Equal(t, 0, int(ct.MinOut), "client stats should only report increased byte count")
			assert.Equal(t, 0, int(ct.MinIn), "client stats should only report increased byte count")
			assert.Equal(t, 96, int(ct.MaxOut), "client stats should only report increased byte count")
			assert.Equal(t, 176, int(ct.MaxIn), "client stats should only report increased byte count")
			assert.Equal(t, 96, int(ct.TotalOut), "client stats should only report increased byte count")
			assert.Equal(t, 176, int(ct.TotalIn), "client stats should only report increased byte count")
		}

		if assert.NotNil(t, st) {
			assert.Equal(t, ct.TotalOut, st.TotalIn, "should report server stats with bytes in")
			assert.Equal(t, ct.TotalIn, st.TotalOut, "should report server stats with bytes out")
		}
	}
}

func startWithMockReporter() (*Measured, *mockReporter) {
	nr := mockReporter{
		traffic: make(map[string]*TrafficTracker),
	}
	md := New(50000)
	md.Start(50*time.Millisecond, &nr)
	// To make sure it really started
	runtime.Gosched()
	return md, &nr
}

type mockReporter struct {
	sync.Mutex
	traffic map[string]*TrafficTracker
}

func (nr *mockReporter) ReportTraffic(t map[string]*TrafficTracker) error {
	nr.Lock()
	defer nr.Unlock()
	for key, value := range t {
		nr.traffic[key] = value
	}
	return nil
}
