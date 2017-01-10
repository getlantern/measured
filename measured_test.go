package measured

import (
	"github.com/getlantern/mockconn"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestMeasuredConn(t *testing.T) {
	rateInterval := 50 * time.Millisecond
	sd := mockconn.SucceedingDialer([]byte("1234567890"))
	wrapped, err := sd.Dial("", "")
	if !assert.NoError(t, err) {
		return
	}
	conn := Wrap(wrapped, rateInterval)
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

	assert.Nil(t, conn.GetFirstError())

	stats := conn.GetStats()
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
