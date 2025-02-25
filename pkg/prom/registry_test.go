package prom_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastly/fastly-exporter/pkg/filter"
	"github.com/fastly/fastly-exporter/pkg/prom"
	"github.com/prometheus/client_golang/prometheus"
)

func TestRegistryEndpoints(t *testing.T) {
	t.Parallel()

	var (
		version          = "dev"
		namespace        = "fastly"
		subsystem        = "rt"
		metricNameFilter = filter.Filter{}
		registry         = prom.NewRegistry(version, namespace, subsystem, metricNameFilter)
	)

	registry.MetricsFor("AAA").RequestsTotal.With(prometheus.Labels{
		"service_id": "AAA", "service_name": "Service One", "datacenter": "NYC",
	}).Add(1)

	registry.MetricsFor("BBB").RequestsTotal.With(prometheus.Labels{
		"service_id": "BBB", "service_name": "Service Two", "datacenter": "NYC",
	}).Add(2)

	server := httptest.NewServer(registry)
	defer server.Close()

	get := func(path string) (body string) {
		t.Helper()

		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}

		if want, have := http.StatusOK, resp.StatusCode; want != have {
			t.Fatalf("code: want %d, have %d", want, have)
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}

		return string(buf)
	}

	expect := func(b bool, msg string) {
		t.Helper()
		if !b {
			t.Error(msg)
		}
	}

	checkMetrics := func(body string, want, dont []string) {
		wantmap := make(map[string]struct{}, len(want))
		for _, s := range want {
			wantmap[s] = struct{}{}
		}

		dontmap := make(map[string]struct{}, len(dont))
		for _, s := range dont {
			dontmap[s] = struct{}{}
		}

		lines := strings.Split(body, "\n")
		for _, line := range lines {
			for s := range wantmap {
				if strings.HasPrefix(line, s) {
					delete(wantmap, s)
				}
			}
			for s := range dontmap {
				if strings.HasPrefix(line, s) {
					t.Errorf("extra: %s", line)
				}
			}
		}
		for s := range wantmap {
			t.Errorf("missing: %s", s)
		}
	}

	t.Run("index", func(t *testing.T) {
		body := get("/")
		expect(strings.Contains(body, "AAA"), "AAA missing")
		expect(strings.Contains(body, "BBB"), "BBB missing")
	})

	t.Run("sd", func(t *testing.T) {
		body := get("/sd")
		expect(strings.Contains(body, "AAA"), "AAA missing")
		expect(strings.Contains(body, "BBB"), "BBB missing")
	})

	t.Run("metrics", func(t *testing.T) {
		body := get("/metrics")
		want, dont := []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="AAA",service_name="Service One"} 1`,
			`fastly_rt_requests_total{datacenter="NYC",service_id="BBB",service_name="Service Two"} 2`,
		}, []string{}
		checkMetrics(body, want, dont)
	})

	t.Run("metrics?target=AAA", func(t *testing.T) {
		body := get("/metrics?target=AAA")
		want, dont := []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="AAA",service_name="Service One"} 1`,
		}, []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="BBB",service_name="Service Two"} 2`,
		}
		checkMetrics(body, want, dont)
	})

	t.Run("metrics?target=BBB", func(t *testing.T) {
		body := get("/metrics?target=BBB")
		want, dont := []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="BBB",service_name="Service Two"} 2`,
		}, []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="AAA",service_name="Service One"} 1`,
		}
		checkMetrics(body, want, dont)
	})

	t.Run("metrics?target=CCC", func(t *testing.T) {
		body := get("/metrics?target=CCC")
		want, dont := []string{}, []string{
			`fastly_rt_requests_total{datacenter="NYC",service_id="AAA",service_name="Service One"} 1`,
			`fastly_rt_requests_total{datacenter="NYC",service_id="BBB",service_name="Service Two"} 2`,
		}
		checkMetrics(body, want, dont)
	})
}
