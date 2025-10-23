package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, s string) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("test JSON invalid: %v", err)
	}
	return []byte(s)
}

func newTestStore() *Store {
	return &Store{
		db:    nil,
		cache: make(map[string]json.RawMessage),
	}
}

func TestMinimalValidateOrder_OK(t *testing.T) {
	payload := mustJSON(t, `{
	  "order_uid": "b563feb7b2b84b6test",
	  "delivery": {"name":"Ivan","phone":"+70000000000","zip":"123","city":"Moscow","address":"Lenina 1","region":"MSK","email":"i@ex.com"},
	  "payment": {"transaction":"t1","request_id":"r1","currency":"RUB","provider":"bank","amount":100,"payment_dt":1711111111,"bank":"bank","delivery_cost":10,"goods_total":90,"custom_fee":0},
	  "items": [{"chrt_id":1,"track_number":"TN1","price":90,"rid":"rid","name":"Item","sale":0,"size":"L","total_price":90,"nm_id":1,"brand":"B","status":1}],
	  "locale":"ru","internal_signature":"","customer_id":"c1","delivery_service":"svc","shardkey":"sh","sm_id":1,"date_created":"2021-07-25T12:00:00","oof_shard":"o1"
	}`)
	id, err := minimalValidateOrder(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "b563feb7b2b84b6test" {
		t.Fatalf("wrong id: %s", id)
	}
}

func TestMinimalValidateOrder_InvalidJSON(t *testing.T) {
	_, err := minimalValidateOrder([]byte(`not json`))
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestMinimalValidateOrder_MissingID(t *testing.T) {
	payload := mustJSON(t, `{
	  "delivery": {}, "payment": {}, "items": [{}]
	}`)
	_, err := minimalValidateOrder(payload)
	if err == nil || !strings.Contains(err.Error(), "missing order_uid") {
		t.Fatalf("expected missing order_uid, got %v", err)
	}
}

func TestMinimalValidateOrder_MissingNested(t *testing.T) {
	payload := mustJSON(t, `{
	  "order_uid": "x",
	  "delivery": {"x":1},
	  "payment": null,
	  "items": []
	}`)
	_, err := minimalValidateOrder(payload)
	if err == nil || !strings.Contains(err.Error(), "missing required nested fields") {
		t.Fatalf("expected nested fields error, got %v", err)
	}
}

func TestAPIHandler_Found(t *testing.T) {
	store := newTestStore()
	store.cache["b563"] = json.RawMessage(`{"order_uid":"b563"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/orders/b563", nil)
	rr := httptest.NewRecorder()

	apiHandler(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), `"order_uid":"b563"`) {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestAPIHandler_NotFound(t *testing.T) {
	store := newTestStore()
	req := httptest.NewRequest(http.MethodGet, "/api/orders/unknown", nil)
	rr := httptest.NewRecorder()

	apiHandler(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rr.Code)
	}
}

func TestAPIHandler_BadRequest(t *testing.T) {
	store := newTestStore()
	req := httptest.NewRequest(http.MethodGet, "/api/orders/", nil)
	rr := httptest.NewRecorder()

	apiHandler(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rr.Code)
	}
}

func TestPageHandler_Found(t *testing.T) {
	store := newTestStore()
	store.cache["b563"] = json.RawMessage(`{
	  "order_uid":"b563",
	  "delivery":{"name":"Ivan","phone":"+7","zip":"1","city":"Moscow","address":"Lenina 1","region":"MSK","email":"i@ex.com"},
	  "payment":{"transaction":"t1","request_id":"r1","currency":"RUB","provider":"bank","amount":100,"payment_dt":1,"bank":"b","delivery_cost":10,"goods_total":90,"custom_fee":0},
	  "items":[{"chrt_id":1,"track_number":"TN","price":90,"rid":"r","name":"Item","sale":0,"size":"L","total_price":90,"nm_id":1,"brand":"B","status":1}],
	  "locale":"ru","internal_signature":"","customer_id":"c","delivery_service":"svc","shardkey":"s","sm_id":1,"date_created":"2021-07-25T12:00:00","oof_shard":"o"
	}`)
	req := httptest.NewRequest(http.MethodGet, "/orders?id=b563", nil)
	rr := httptest.NewRecorder()

	pageHandler(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	html := rr.Body.String()
	if !strings.Contains(html, "Заказ:") || !strings.Contains(html, "b563") {
		t.Fatalf("page content missing order info:\n%s", html)
	}
}

func TestPageHandler_NotFound(t *testing.T) {
	store := newTestStore()
	req := httptest.NewRequest(http.MethodGet, "/orders?id=unknown", nil)
	rr := httptest.NewRecorder()

	pageHandler(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "не найден") {
		t.Fatalf("expected 'не найден' message, got:\n%s", rr.Body.String())
	}
}