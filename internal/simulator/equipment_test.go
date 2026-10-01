package simulator

import (
	"context"
	"encoding/json"
	"factory.local/factory-platform/internal/equipment"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestCatalogOperations(t *testing.T) {
	for _, d := range equipment.Catalog() {
		for _, product := range []string{"board-a", "board-b"} {
			t.Run(d.ID+"/"+product, func(t *testing.T) {
				s, err := New(Config{EquipmentID: d.ID, Product: product, Revision: "revision-123", InitialMode: ModeNormal})
				if err != nil {
					t.Fatal(err)
				}
				assertStatusCode(t, s, http.MethodGet, "/readyz", nil, http.StatusOK)
				response := httptest.NewRecorder()
				s.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/operations", nil))
				var result Operation
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if response.Code != 200 || result.EquipmentID != d.ID || result.Revision != "revision-123" || result.Product != product || result.Workstation != d.Workstation || result.Sequence != 1 || !result.Accepted {
					t.Fatalf("operation = %+v, code=%d", result, response.Code)
				}
				assertStatusCode(t, s, http.MethodPost, "/api/v1/measurements", nil, http.StatusConflict)
			})
		}
	}
}

func TestSetupIsSeparateFromCalibration(t *testing.T) {
	s, err := New(Config{EquipmentID: "manual-soldering-01", Product: "board-a", InitialMode: ModeUncalibrated, RequireSetup: true})
	if err != nil {
		t.Fatal(err)
	}
	assertStatusCode(t, s, "GET", "/healthz", nil, 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 503)
	assertStatusCode(t, s, "POST", "/api/v1/operations", nil, 409)
	assertStatusCode(t, s, "POST", "/api/v1/calibrate", nil, 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 503)
	assertStatusCode(t, s, "POST", "/api/v1/setup", nil, 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 200)
	assertStatusCode(t, s, "PUT", "/api/v1/mode", []byte(`{"mode":"unprepared"}`), 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 503)
	assertStatusCode(t, s, "PUT", "/api/v1/mode", []byte(`{"mode":"normal"}`), 200)
	assertStatusCode(t, s, "GET", "/readyz", nil, 503)
	assertStatusCode(t, s, "POST", "/api/v1/setup", nil, 200)
	assertStatusCode(t, s, "PUT", "/api/v1/mode", []byte(`{"mode":"fault"}`), 200)
	assertStatusCode(t, s, "POST", "/api/v1/setup", nil, 409)
	assertStatusCode(t, s, "POST", "/api/v1/operations", nil, 409)
	assertStatusCode(t, s, "GET", "/healthz", nil, 200)
}

type notifyingContext struct {
	context.Context
	started chan struct{}
	once    sync.Once
}

func (c *notifyingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.started) })
	return c.Context.Done()
}

func TestFaultDuringOperationDoesNotProduceObservation(t *testing.T) {
	s, err := New(Config{EquipmentID: "reflow-oven-01", Product: "board-a", InitialMode: ModeSlow, Latency: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &notifyingContext{Context: context.Background(), started: make(chan struct{})}
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/operations", nil).WithContext(ctx))
	}()
	<-ctx.started
	assertStatusCode(t, s, "PUT", "/api/v1/mode", []byte(`{"mode":"fault"}`), 200)
	<-done
	if response.Code != 409 || s.snapshot().Sequence != 0 {
		t.Fatalf("operation survived fault: %s", response.Body.String())
	}
}

func TestCatalogConfigurationRejectsMistakes(t *testing.T) {
	for _, cfg := range []Config{
		{EquipmentID: "unknown", Product: "board-a", InitialMode: ModeNormal},
		{EquipmentID: "programmer-01", Product: "typo", InitialMode: ModeNormal},
		{EquipmentID: "programmer-01", Kind: "camera", Product: "board-a", InitialMode: ModeNormal},
		{Kind: "typo", Product: "board-a", InitialMode: ModeNormal},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}
