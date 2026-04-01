/*
Copyright 2026 The Kubernetes resource-state-metrics Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/common/expfmt"
	"k8s.io/apimachinery/pkg/types"
)

// TestMetricsHandlerOpenMetricsEOF verifies that the /metrics handler appends
// # EOF when the client negotiates OpenMetrics, and omits it for plain text.
func TestMetricsHandlerOpenMetricsEOF(t *testing.T) {
	t.Parallel()

	// Replicate the metricsHandler closure from mainServer.build().
	var binarySemaphore sync.RWMutex

	metricsHandler := func(generator func(w http.ResponseWriter)) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			binarySemaphore.RLock()
			defer binarySemaphore.RUnlock()

			contentType := expfmt.NegotiateIncludingOpenMetrics(request.Header)
			if contentType.FormatType() != expfmt.TypeOpenMetrics {
				contentType = expfmt.NewFormat(expfmt.TypeTextPlain)
			}

			writer.Header().Set("Content-Type", string(contentType))
			generator(writer)

			if contentType.FormatType() == expfmt.TypeOpenMetrics {
				fmt.Fprint(writer, "# EOF\n")
			}
		}
	}

	store := &StoreType{
		headers: []string{"# HELP kube_customresource_test A test metric\n# TYPE kube_customresource_test gauge"},
		metrics: map[types.UID][]string{
			"uid1": {"kube_customresource_test{name=\"a\"} 1\n"},
		},
	}

	handler := metricsHandler(func(w http.ResponseWriter) {
		_ = newMetricsWriter(store).writeStores(w)
	})

	tests := []struct {
		name      string
		accept    string
		expectEOF bool
	}{
		{
			name:      "OpenMetrics request ends with EOF",
			accept:    "application/openmetrics-text;version=1.0.0,text/plain;version=0.0.4;q=0.5",
			expectEOF: true,
		},
		{
			name:      "plain text request has no EOF",
			accept:    "text/plain;version=0.0.4",
			expectEOF: false,
		},
		{
			name:      "no Accept header defaults to text, no EOF",
			accept:    "",
			expectEOF: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			body := rec.Body.String()
			endsWithEOF := strings.HasSuffix(body, "# EOF\n")

			if tt.expectEOF && !endsWithEOF {
				t.Errorf("expected response to end with # EOF, got:\n%s", body)
			}

			if !tt.expectEOF && endsWithEOF {
				t.Errorf("expected response NOT to end with # EOF, got:\n%s", body)
			}
		})
	}
}
