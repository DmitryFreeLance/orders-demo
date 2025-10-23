package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	stan "github.com/nats-io/stan.go"
)

type Delivery struct {
	Name, Phone, Zip, City, Address, Region, Email string
}
type Payment struct {
	Transaction, RequestID, Currency, Provider, Bank string
	Amount, PaymentDT, DeliveryCost, GoodsTotal, CustomFee int64
}
type Item struct {
	ChrtID int64  `json:"chrt_id"`
	TrackNumber string `json:"track_number"`
	Price int64 `json:"price"`
	RID string `json:"rid"`
	Name string `json:"name"`
	Sale int64 `json:"sale"`
	Size string `json:"size"`
	TotalPrice int64 `json:"total_price"`
	NMID int64 `json:"nm_id"`
	Brand string `json:"brand"`
	Status int64 `json:"status"`
}
type Order struct {
	OrderUID, TrackNumber, Entry, Locale, InternalSign, CustomerID, DeliveryService, ShardKey, DateCreated, OOFShard string
	Delivery Delivery
	Payment  Payment
	Items    []Item
	SmID     int64 `json:"sm_id"`
}

type Store struct {
	db    *pgxpool.Pool
	mu    sync.RWMutex
	cache map[string]json.RawMessage
}

func mustEnv(key, def string) string {
	if v := os.Getenv(key); v != "" { return v }
	return def
}

func newStore(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil { return nil, err }
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil { return nil, err }
	s := &Store{db: pool, cache: make(map[string]json.RawMessage)}
	if err := s.initDB(ctx); err != nil { return nil, err }
	if err := s.LoadCache(ctx); err != nil { return nil, err }
	return s, nil
}
func (s *Store) Close(){ if s.db != nil { s.db.Close() } }
func (s *Store) initDB(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS orders (
  order_uid  TEXT PRIMARY KEY,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_orders_uid ON orders(order_uid);`
	_, err := s.db.Exec(ctx, ddl)
	return err
}
func (s *Store) LoadCache(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT order_uid, payload FROM orders`)
	if err != nil { return err }
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id string; var payload []byte
		if err := rows.Scan(&id, &payload); err != nil { return err }
		s.mu.Lock(); s.cache[id] = json.RawMessage(payload); s.mu.Unlock(); n++
	}
	log.Printf("cache restored: %d orders", n)
	return rows.Err()
}
func (s *Store) Upsert(ctx context.Context, id string, payload []byte) error {
	_, err := s.db.Exec(ctx, `INSERT INTO orders(order_uid, payload) VALUES($1,$2)
	ON CONFLICT(order_uid) DO UPDATE SET payload=EXCLUDED.payload`, id, payload)
	if err != nil { return err }
	s.mu.Lock(); s.cache[id] = json.RawMessage(payload); s.mu.Unlock()
	return nil
}
func (s *Store) Get(id string) (json.RawMessage, bool) {
	s.mu.RLock(); defer s.mu.RUnlock()
	val, ok := s.cache[id]; return val, ok
}

// --- validation ---
func minimalValidateOrder(payload []byte) (string, error) {
	if !json.Valid(payload) { return "", errors.New("invalid JSON") }
	var tmp struct {
		OrderUID string `json:"order_uid"`
		Delivery *json.RawMessage `json:"delivery"`
		Payment  *json.RawMessage `json:"payment"`
		Items    []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(payload, &tmp); err != nil { return "", err }
	if tmp.OrderUID == "" { return "", errors.New("missing order_uid") }
	if tmp.Delivery == nil || tmp.Payment == nil || len(tmp.Items) == 0 {
		return "", errors.New("missing required nested fields")
	}
	return tmp.OrderUID, nil
}

// --- HTTP ---
func apiHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/orders/")
		if id == "" || id == r.URL.Path { http.Error(w, "missing id", http.StatusBadRequest); return }
		if payload, ok := store.Get(id); ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK); _, _ = w.Write(payload); return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}
}

var pageTpl = template.Must(template.New("page").Parse(`
<!doctype html><html lang="ru"><head>
<meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>Order Viewer</title>
<style>
body{font-family:system-ui,-apple-system,Segoe UI,Roboto,Arial,sans-serif;margin:24px}
.card{max-width:900px;margin:0 auto;border:1px solid #ddd;border-radius:8px;padding:16px}
.row{display:flex;gap:24px;flex-wrap:wrap}.col{flex:1;min-width:260px}.muted{color:#666;font-size:.9em}
pre{background:#f7f7f7;padding:12px;overflow:auto;border-radius:6px}
input[type=text]{width:420px;padding:8px}button{padding:8px 12px;cursor:pointer}
</style></head><body><div class="card">
<h2>Поиск заказа</h2>
<form method="GET" action="/orders">
  <input name="id" type="text" placeholder="order_uid" value="{{.ID}}"/>
  <button type="submit">Показать</button>
  <span class="muted">пример: b563feb7b2b84b6test</span>
</form>
{{if .Found}}
  <hr/><h3>Заказ: <code>{{.Order.OrderUID}}</code></h3>
  <div class="row">
    <div class="col"><h4>Доставка</h4>
      <div class="muted">{{.Order.Delivery.Name}}, {{.Order.Delivery.Phone}}</div>
      <div>{{.Order.Delivery.Address}}, {{.Order.Delivery.City}} {{.Order.Delivery.Zip}}</div>
      <div>{{.Order.Delivery.Region}}</div>
      <div>{{.Order.Delivery.Email}}</div>
    </div>
    <div class="col"><h4>Оплата</h4>
      <div>Провайдер: {{.Order.Payment.Provider}}</div>
      <div>Валюта: {{.Order.Payment.Currency}}</div>
      <div>Сумма: {{.Order.Payment.Amount}}</div>
      <div>Доставка: {{.Order.Payment.DeliveryCost}}</div>
      <div>Товары: {{.Order.Payment.GoodsTotal}}</div>
      <div class="muted">Транзакция: {{.Order.Payment.Transaction}}</div>
    </div>
  </div>
  <h4>Товары ({{len .Order.Items}})</h4>
  <ul>{{range .Order.Items}}
    <li>{{.Name}} ({{.Brand}}) — {{.TotalPrice}} | статус {{.Status}}</li>
  {{end}}</ul>
  <details><summary>Показать сырой JSON</summary><pre>{{.Raw}}</pre></details>
{{else if .ID}}
  <hr/><div>Заказ с id <code>{{.ID}}</code> не найден в кэше.</div>
{{end}}
</div></body></html>
`))

func pageHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		var data struct{ ID string; Found bool; Order Order; Raw string }
		data.ID = id
		if id != "" {
			if payload, ok := store.Get(id); ok {
				data.Found = true; data.Raw = string(payload)
				_ = json.Unmarshal(payload, &data.Order)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTpl.Execute(w, data)
	}
}

func main() {
	addr := mustEnv("HTTP_ADDR", ":8080")
	dsn := mustEnv("PG_DSN", "postgres://app:app@db:5432/orders?sslmode=disable")
	natsURL := mustEnv("NATS_URL", "nats://nats-streaming:4222")
	clusterID := mustEnv("STAN_CLUSTER_ID", "test-cluster")
	clientID := mustEnv("STAN_CLIENT_ID", "orders-service-1")
	channel := mustEnv("STAN_CHANNEL", "orders")
	durable := mustEnv("STAN_DURABLE", "orders-durable")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM); defer cancel()

	store, err := newStore(ctx, dsn)
	if err != nil { log.Fatalf("db init: %v", err) }
	defer store.Close()

	sc, err := stan.Connect(clusterID, clientID, stan.NatsURL(natsURL), stan.SetConnectionLostHandler(
		func(_ stan.Conn, reason error) { log.Printf("stan connection lost: %v", reason) }))
	if err != nil { log.Fatalf("stan connect: %v", err) }
	defer sc.Close()

	_, err = sc.QueueSubscribe(channel, "workers", func(m *stan.Msg) {
		id, vErr := minimalValidateOrder(m.Data)
		if vErr != nil { log.Printf("drop invalid msg (seq=%d): %v", m.Sequence, vErr); _ = m.Ack(); return }
		if err := store.Upsert(context.Background(), id, m.Data); err != nil {
			log.Printf("db upsert failed (seq=%d, id=%s): %v", m.Sequence, id, err)
			return
		}
		_ = m.Ack()
		log.Printf("stored order id=%s (seq=%d)", id, m.Sequence)
	},
		stan.DurableName(durable),
		stan.DeliverAllAvailable(),
		stan.SetManualAckMode(),
		stan.AckWait(30*time.Second),
		stan.MaxInflight(1),
	)
	if err != nil { log.Fatalf("stan subscribe: %v", err) }

	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders/", apiHandler(store))
	mux.HandleFunc("/orders", pageHandler(store))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request){ http.Redirect(w, r, "/orders", http.StatusFound) })
	srv := &http.Server{ Addr: addr, Handler: mux, ReadHeaderTimeout: 5*time.Second }

	go func(){
		log.Printf("http listen on %s", addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) && err != nil {
			log.Fatalf("http: %v", err)
		}
	}()
	<-ctx.Done()
	log.Printf("shutting down...")
	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second); defer cancel2()
	_ = srv.Shutdown(shutdownCtx)
	_ = sql.ErrNoRows
}