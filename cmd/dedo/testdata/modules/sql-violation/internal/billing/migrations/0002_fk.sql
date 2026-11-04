ALTER TABLE billing.invoices ADD FOREIGN KEY (order_id) REFERENCES "orders".orders (id);
