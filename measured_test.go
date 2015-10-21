package measured

import (
	"fmt"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/getlantern/testify/assert"
)

type mockReporter struct {
	s []*Stats
}

func (nr *mockReporter) Submit(s *Stats) error {
	nr.s = append(nr.s, s)
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
		assert.Equal(t, "errors", nr.s[0].Type, "should report correct remoteAddr")
		assert.Equal(t, "localhost:9999", nr.s[0].Tags["remoteAddr"], "should report correct remoteAddr")
		assert.Equal(t, "connection refused", nr.s[0].Tags["error"], "should report connection reset")
		assert.Equal(t, 1, nr.s[0].Fields["value"], "should report connection reset")

		assert.Equal(t, "errors", nr.s[1].Type, "should report correct remoteAddr")
		assert.Equal(t, "localhost:9998", nr.s[1].Tags["remoteAddr"], "should report correct remoteAddr")
		assert.Equal(t, "connection refused", nr.s[1].Tags["error"], "should report connection reset")
		assert.Equal(t, 1, nr.s[1].Fields["value"], "should report connection reset")
	}
}

func TestDefaultTags(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	SetDefaults(map[string]string{"app": "test-app"})
	reportError("test-remoteAddr", fmt.Errorf("test-error"), "dial-phase")
	time.Sleep(100 * time.Millisecond)
	if assert.Equal(t, 1, len(nr.s)) {
		assert.Equal(t, "test-app", nr.s[0].Tags["app"], "should report with default tags")
	}
}

func TestReportStats(t *testing.T) {
	nr := startWithMockReporter()
	defer Stop()
	var bytesIn, bytesOut uint64
	var remoteAddr string

	// start server with byte counting
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if assert.NoError(t, err, "Listen should not fail") {
		// large enough interval so it will only report stats in Close()
		ml := Listener(l, 10*time.Second)
		s := http.Server{
			Handler: http.NotFoundHandler(),
			ConnState: func(c net.Conn, s http.ConnState) {
				if s == http.StateIdle {
					remoteAddr = c.RemoteAddr().String()
					mc := c.(*MeasuredConn)
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
		assert.Equal(t, "stats", nr.s[0].Type, "should report server stats")
		assert.Equal(t, remoteAddr, nr.s[0].Tags["remoteAddr"], "should report server stats with remote addr")
		assert.Equal(t, bytesIn, nr.s[0].Fields["bytesIn"], "should report server stats with bytes in")
		assert.Equal(t, bytesOut, nr.s[0].Fields["bytesOut"], "should report server stats with bytes out")

		assert.Equal(t, "stats", nr.s[1].Type, "should report client stats each interval")
		assert.Equal(t, l.Addr().String(), nr.s[1].Tags["remoteAddr"], "should report server as remote addr")
		assert.Equal(t, bytesIn, nr.s[1].Fields["bytesOut"], "should report same byte count as server")
		assert.Equal(t, bytesOut, nr.s[1].Fields["bytesIn"], "should report same byte count as server")

		assert.Equal(t, "stats", nr.s[2].Type, "should report client stats when close")
		assert.Equal(t, l.Addr().String(), nr.s[2].Tags["remoteAddr"], "should report server as remote addr")
		assert.Equal(t, uint64(0), nr.s[2].Fields["bytesOut"], "should only report increased byte count")
		assert.Equal(t, uint64(0), nr.s[2].Fields["bytesIn"], "should only report increased byte count")
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
