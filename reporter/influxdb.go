package reporter

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/getlantern/measured"
	"github.com/influxdb/influxdb/client"
)

type influxDBReporter struct {
	c  *client.Client
	db string
}

func NewInfluxDBReporter(dbUrl, user, password, db string, httpClient *http.Client) measured.Reporter {
	u, err := url.Parse(dbUrl)
	if err != nil {
		log.Fatal(err)
	}
	conf := client.Config{
		URL:      *u,
		Username: user,
		Password: password,
	}
	c, err := client.NewClient(conf)
	if err != nil {
		log.Fatal(err)
	}
	if httpClient != nil {
		c.HttpClient = httpClient
	}
	return &influxDBReporter{c, db}
}

func (ir *influxDBReporter) Submit(s measured.Stats) error {
	var pts []client.Point
	for k, v := range s.Errors {
		pts = append(pts, client.Point{
			Measurement: "errors",
			Tags: map[string]string{
				"instance": s.Instance,
				"server":   s.Server,
				"error":    k,
			},
			Fields: map[string]interface{}{
				"value": v,
			},
			Time:      time.Now(),
			Precision: "s",
		})
	}
	bps := client.BatchPoints{
		Points:          pts,
		Database:        ir.db,
		RetentionPolicy: "default",
	}
	_, err := ir.c.Write(bps)
	return err
}
