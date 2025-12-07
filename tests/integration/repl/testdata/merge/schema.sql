-- Common schema for bidirectional merge tests
-- This file is used by all merge integration tests

-- =============================================================================
-- Basic Test Tables
-- =============================================================================

-- Simple table with single-column PK
CREATE TABLE IF NOT EXISTS public.users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    version TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- Table with composite primary key
CREATE TABLE IF NOT EXISTS public.order_items (
    order_id INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    price DECIMAL(10,2),
    PRIMARY KEY (order_id, item_id)
);

-- =============================================================================
-- Foreign Key Test Tables (for FK ordering tests)
-- =============================================================================

-- Parent table: customers
CREATE TABLE IF NOT EXISTS public.customers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);

-- Child table: orders (references customers)
CREATE TABLE IF NOT EXISTS public.orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES public.customers(id),
    total DECIMAL(10,2),
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Grandchild table: order_details (references orders)
CREATE TABLE IF NOT EXISTS public.order_details (
    id SERIAL PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES public.orders(id),
    product_name TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price DECIMAL(10,2)
);

-- =============================================================================
-- Products table (independent, for multi-table tests)
-- =============================================================================

CREATE TABLE IF NOT EXISTS public.products (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    price DECIMAL(10,2),
    category TEXT,
    stock INTEGER DEFAULT 0
);
