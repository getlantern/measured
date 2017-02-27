package measured

import (
	"net"
	"testing"
	"time"

	"github.com/getlantern/mockconn"
	"github.com/stretchr/testify/assert"
)

func TestMeasuredConn(t *testing.T) {
	rateInterval := 50 * time.Millisecond
	sd := mockconn.SucceedingDialer([]byte("1234567890"))
	wrapped, err := sd.Dial("", "")
	if !assert.NoError(t, err) {
		return
	}
	conn := Wrap(&slowConn{wrapped}, rateInterval, nil)
	n, err := conn.Write([]byte("12345678"))
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, 8, n)
	b := make([]byte, 1000)
	n, err = conn.Read(b)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, 10, n)
	// Be inactive for a bit
	time.Sleep(3 * rateInterval)
	conn.Close()

	// Wait for tracking to finish
	time.Sleep(2 * rateInterval)

	assert.Nil(t, conn.FirstError())

	stats := conn.Stats()
	if !assert.NotNil(t, stats) {
		return
	}

	assert.Equal(t, 8, stats.SentTotal)
	assert.True(t, stats.SentMin > 0)
	assert.True(t, stats.SentMax > 0)
	assert.True(t, stats.SentAvg > 0)

	assert.Equal(t, 10, stats.RecvTotal)
	assert.True(t, stats.RecvMin > 0)
	assert.True(t, stats.RecvMax > 0)
	assert.True(t, stats.RecvAvg > 0)
}

type slowConn struct {
	net.Conn
}

func (c *slowConn) Write(b []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return c.Conn.Write(b)
}

func (c *slowConn) Read(b []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return c.Conn.Read(b)
}
