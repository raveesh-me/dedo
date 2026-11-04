SELECT o.id, i.total FROM orders.orders o JOIN billing.invoices i ON i.order_id = o.id;
