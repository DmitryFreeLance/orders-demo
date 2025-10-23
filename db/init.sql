-- db/init.sql
CREATE USER app WITH PASSWORD 'app';
CREATE DATABASE orders OWNER app;

\connect orders

CREATE TABLE IF NOT EXISTS orders (
  order_uid  TEXT PRIMARY KEY,
  payload    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_uid ON orders(order_uid);