package reporter

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getlantern/golog"
	"github.com/getlantern/measured"
)

var (
	log = golog.LoggerFor("measured.reporter")
)

type influxDBReporter struct {
	httpClient *http.Client
	url        string
	username   string
	password   string
}

func NewInfluxDBReporter(influxURL, username, password, dbName string, httpClient *http.Client) measured.Reporter {
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}
	}
	u := fmt.Sprintf("%s/write?db=%s", strings.TrimRight(influxURL, "/"), dbName)
	return &influxDBReporter{httpClient, u, username, password}
}

func (ir *influxDBReporter) Submit(s measured.Stats) error {
	var buf bytes.Buffer
	for k, v := range s.Errors {
		buf.WriteString("errors,")
		buf.WriteString(fmt.Sprintf("instance=%s,server=%s,error=%s ", s.Instance, s.Server, escapeStringField(k)))
		buf.WriteString(fmt.Sprintf("value=%di %d\n", v, time.Now().UnixNano()))
	}
	req, err := http.NewRequest("POST", ir.url, &buf)
	if err != nil {
		log.Errorf("Error make POST request to %s: %s", ir.url, err)
		return err
	}
	req.SetBasicAuth(ir.username, ir.password)
	rsp, err := ir.httpClient.Do(req)
	if err != nil {
		log.Errorf("Error send POST request to %s: %s", ir.url, err)
		return err
	}
	if rsp.StatusCode != 204 {
		err = fmt.Errorf("Error response from %s: %s", ir.url, rsp.Status)
		log.Error(err)
		return err
	}
	return err
}

func escapeStringField(in string) string {
	var out []byte
	i := 0
	for {
		if i >= len(in) {
			break
		}
		if in[i] == ',' || in[i] == '=' || in[i] == ' ' {
			out = append(out, '\\')
			out = append(out, in[i])
			i += 1
			continue
		}
		out = append(out, in[i])
		i += 1

	}
	return string(out)
}
