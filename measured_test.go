package measured

import (
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/getlantern/testify/assert"
)

type mockReporter struct {
	s []Stat
}

func (nr *mockReporter) ReportError(e *Error) error {
	nr.s = append(nr.s, e)
	return nil
}

func (nr *mockReporter) ReportLatency(e *Latency) error {
	nr.s = append(nr.s, e)
	return nil
}

func (nr *mockReporter) ReportTraffic(e *Traffic) error {
	nr.s = append(nr.s, e)
	return nil
}

func TestReportError(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	d := Dialer(net.Dial, 10*time.Second)
	_, _ = d("tcp", "localhost:9999")
	_, _ = d("tcp", "localhost:9998")
	time.Sleep(100 * time.Millisecond)
	if assert.Equal(t, 2, len(nr.s)) {
		e := nr.s[0].(*Error)
		assert.Equal(t, "localhost:9999", e.ID, "should report correct RemoteAddr")
		assert.Equal(t, "connection refused", e.Error, "should report connection reset")

		e = nr.s[1].(*Error)
		assert.Equal(t, "localhost:9998", e.ID, "should report correct RemoteAddr")
		assert.Equal(t, "connection refused", e.Error, "should report connection reset")
	}
}

func TestReportStats(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	var bytesIn, bytesOut uint64
	var RemoteAddr string

	// start server with byte counting
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if assert.NoError(t, err, "Listen should not fail") {
		// large enough interval so it will only report stats in Close()
		ml := Listener(l, 10*time.Second)
		s := http.Server{
			Handler: http.NotFoundHandler(),
			ConnState: func(c net.Conn, s http.ConnState) {
				if s == http.StateIdle {
					RemoteAddr = c.RemoteAddr().String()
					mc := c.(*Conn)
					bytesIn = mc.BytesIn
					bytesOut = mc.BytesOut
					mc.Close()
				}
			},
		}
		go s.Serve(ml)
	}

	// start client with byte counting
	c := http.Client{
		Transport: &http.Transport{
			// carefully chosen interval to report another once before Close()
			Dial: Dialer(net.Dial, 60*time.Millisecond),
		},
	}
	req, _ := http.NewRequest("GET", "http://"+l.Addr().String(), nil)
	resp, _ := c.Do(req)
	assert.Equal(t, 404, resp.StatusCode)
	resp.Body.Close()
	assert.Equal(t, uint64(97), bytesIn, "")
	assert.Equal(t, uint64(143), bytesOut, "")

	time.Sleep(100 * time.Millisecond)
	// verify both client and server stats
	if assert.Equal(t, 3, len(nr.s)) {
		e := nr.s[0].(*Traffic)
		assert.Equal(t, RemoteAddr, e.ID, "should report server stats with Remote addr")
		assert.Equal(t, bytesIn, e.BytesIn, "should report server stats with bytes in")
		assert.Equal(t, bytesOut, e.BytesOut, "should report server stats with bytes out")

		e = nr.s[1].(*Traffic)
		assert.Equal(t, l.Addr().String(), e.ID, "should report server as Remote addr")
		assert.Equal(t, bytesIn, e.BytesOut, "should report same byte count as server")
		assert.Equal(t, bytesOut, e.BytesIn, "should report same byte count as server")

		e = nr.s[2].(*Traffic)
		assert.Equal(t, l.Addr().String(), e.ID, "should report server as Remote addr")
		assert.Equal(t, uint64(0), e.BytesOut, "should only report increased byte count")
		assert.Equal(t, uint64(0), e.BytesIn, "should only report increased byte count")
	}
}

func startWithMockReporter() *mockReporter {
	nr := mockReporter{}
	Reset()
	AddReporter(&nr)
	Start()
	// To make sure it really started
	runtime.Gosched()
	return &nr
}
