-- xmdb demo: tiny e-commerce shop
CREATE TABLE customers (
    id         SERIAL PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    full_name  TEXT NOT NULL,
    city       TEXT NOT NULL,
    country    TEXT NOT NULL,
    joined_on  DATE NOT NULL
);

CREATE TABLE products (
    id         SERIAL PRIMARY KEY,
    sku        TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    category   TEXT NOT NULL,
    price      NUMERIC(10,2) NOT NULL,
    stock_qty  INT NOT NULL DEFAULT 0
);

CREATE TABLE orders (
    id           SERIAL PRIMARY KEY,
    customer_id  INT NOT NULL REFERENCES customers(id),
    status       TEXT NOT NULL,
    total        NUMERIC(10,2) NOT NULL DEFAULT 0,
    placed_at    DATE NOT NULL
);

CREATE TABLE order_items (
    id          SERIAL PRIMARY KEY,
    order_id    INT NOT NULL REFERENCES orders(id),
    product_id  INT NOT NULL REFERENCES products(id),
    qty         INT NOT NULL,
    unit_price  NUMERIC(10,2) NOT NULL
);

CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_items_order ON order_items(order_id);
CREATE INDEX idx_items_product ON order_items(product_id);
