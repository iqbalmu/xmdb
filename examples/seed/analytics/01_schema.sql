-- xmdb demo: monthly sales rollup
CREATE TABLE regions (
    id    INT PRIMARY KEY,
    name  VARCHAR(40) NOT NULL
);

CREATE TABLE monthly_sales (
    id         INT PRIMARY KEY,
    region_id  INT NOT NULL,
    month      CHAR(7) NOT NULL,
    orders     INT NOT NULL,
    revenue    DECIMAL(12,2) NOT NULL,
    CONSTRAINT fk_sales_region FOREIGN KEY (region_id) REFERENCES regions(id)
);
