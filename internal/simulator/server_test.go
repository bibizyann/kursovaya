/*
Copyright 2026.

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

package simulator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadinessTracksModeAndCalibration(t *testing.T) {
	t.Parallel()

	server, err := New(Config{Kind: "camera", Product: "board-a", InitialMode: ModeNormal})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	assertStatusCode(t, server, http.MethodGet, "/readyz", nil, http.StatusOK)
	assertStatusCode(t, server, http.MethodPut, "/api/v1/mode", []byte(`{"mode":"uncalibrated"}`), http.StatusOK)
	assertStatusCode(t, server, http.MethodGet, "/readyz", nil, http.StatusServiceUnavailable)
	assertStatusCode(t, server, http.MethodPost, "/api/v1/calibrate", nil, http.StatusOK)
	assertStatusCode(t, server, http.MethodGet, "/readyz", nil, http.StatusOK)
	assertStatusCode(t, server, http.MethodPut, "/api/v1/mode", []byte(`{"mode":"fault"}`), http.StatusOK)
	assertStatusCode(t, server, http.MethodGet, "/healthz", nil, http.StatusOK)
	assertStatusCode(t, server, http.MethodGet, "/readyz", nil, http.StatusServiceUnavailable)
}

func TestMeasurementsAreDeterministic(t *testing.T) {
	t.Parallel()

	server, err := New(Config{Kind: "tester", Product: "board-b", InitialMode: ModeNormal})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for wantSequence := uint64(1); wantSequence <= 10; wantSequence++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/measurements", nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("measurement %d status = %d", wantSequence, response.Code)
		}
		var measurement Measurement
		if err := json.NewDecoder(response.Body).Decode(&measurement); err != nil {
			t.Fatalf("decode measurement: %v", err)
		}
		if measurement.Sequence != wantSequence {
			t.Fatalf("sequence = %d, want %d", measurement.Sequence, wantSequence)
		}
		if measurement.Accepted != (wantSequence%10 != 0) {
			t.Fatalf("accepted = %v for sequence %d", measurement.Accepted, wantSequence)
		}
	}
}

func assertStatusCode(t *testing.T, handler http.Handler, method, path string, body []byte, want int) {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, response.Code, want, response.Body.String())
	}
}
