-- Foreign key relationship test data
-- Tests that merge correctly orders parent tables before children
--
-- Tables: customers -> orders -> order_details
--
-- Node A: customer 100 with orders
-- Node B: customer 200 with orders
--
-- Merge must happen in order: customers, orders, order_details

-- =============================================================================
-- Node A Data
-- =============================================================================

-- Customers on Node A
INSERT INTO public.customers (id, name, email) VALUES
    (100, 'Customer A', 'customer.a@example.com');

-- Orders for Customer A
INSERT INTO public.orders (id, customer_id, total, status) VALUES
    (1000, 100, 199.99, 'completed'),
    (1001, 100, 49.99, 'pending');

-- Order details for Order 1000
INSERT INTO public.order_details (order_id, product_name, quantity, unit_price) VALUES
    (1000, 'Widget A', 2, 99.99),
    (1000, 'Gadget B', 1, 0.01);

-- =============================================================================
-- Node B Data
-- =============================================================================

-- Customers on Node B
INSERT INTO public.customers (id, name, email) VALUES
    (200, 'Customer B', 'customer.b@example.com');

-- Orders for Customer B
INSERT INTO public.orders (id, customer_id, total, status) VALUES
    (2000, 200, 299.99, 'shipped'),
    (2001, 200, 15.00, 'pending');

-- Order details for Order 2000
INSERT INTO public.order_details (order_id, product_name, quantity, unit_price) VALUES
    (2000, 'Product X', 3, 99.99),
    (2000, 'Product Y', 1, 0.02);
