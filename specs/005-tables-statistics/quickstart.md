# Quickstart: Tables & Statistics Viewer

**Feature**: 005-tables-statistics
**Date**: 2025-11-24

## Prerequisites

- Go 1.21+ installed
- PostgreSQL 11+ database for testing
- Steep development environment set up

## Development Setup

### 1. Branch Setup

```bash
# Already on feature branch
git branch
# * 005-tables-statistics

# Ensure dependencies are up to date
go mod tidy
```

### 2. Test Database Setup

Create a test database with sample tables for development:

```sql
-- Connect to PostgreSQL
psql -U postgres

-- Create test database
CREATE DATABASE steep_test;
\c steep_test

-- Create sample schemas
CREATE SCHEMA inventory;
CREATE SCHEMA analytics;

-- Create sample tables
CREATE TABLE public.users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE public.orders (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES public.users(id),
    total DECIMAL(10,2),
    created_at TIMESTAMP DEFAULT NOW()
) PARTITION BY RANGE (created_at);

-- Create partitions
CREATE TABLE public.orders_2024_q1 PARTITION OF public.orders
    FOR VALUES FROM ('2024-01-01') TO ('2024-04-01');
CREATE TABLE public.orders_2024_q2 PARTITION OF public.orders
    FOR VALUES FROM ('2024-04-01') TO ('2024-07-01');

CREATE TABLE inventory.products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    price DECIMAL(10,2),
    stock INT DEFAULT 0
);

-- Create some indexes
CREATE INDEX idx_users_email ON public.users(email);
CREATE INDEX idx_orders_user ON public.orders(user_id);
CREATE INDEX idx_products_name ON inventory.products(name);

-- Insert sample data to generate statistics
INSERT INTO public.users (name, email)
SELECT 'User ' || i, 'user' || i || '@example.com'
FROM generate_series(1, 1000) i;

INSERT INTO public.orders_2024_q1 (user_id, total, created_at)
SELECT (random() * 999 + 1)::int, random() * 100, '2024-01-01'::date + (random() * 89)::int
FROM generate_series(1, 5000);

INSERT INTO inventory.products (name, price, stock)
SELECT 'Product ' || i, random() * 100, (random() * 1000)::int
FROM generate_series(1, 500) i;

-- Generate statistics
ANALYZE;

-- Optional: Install pgstattuple for bloat detection
CREATE EXTENSION IF NOT EXISTS pgstattuple;
```

### 3. Run Steep with Test Database

```bash
# Build steep
make build

# Run with test database
./bin/steep --dsn "postgres://postgres@localhost/steep_test"

# Or with readonly mode (for testing readonly restrictions)
./bin/steep --dsn "postgres://postgres@localhost/steep_test" --readonly
```

### 4. Access Tables View

1. Launch Steep
2. Press `5` to open Tables view
3. Navigate with `j`/`k`
4. Press `Enter` to expand schemas
5. Press `P` to toggle system schemas
6. Press `h` for help

## File Structure

Files to create for this feature:

```
internal/
├── db/
│   ├── models/
│   │   └── table.go           # NEW
│   └── queries/
│       └── tables.go          # NEW
└── ui/
    └── views/
        └── tables/
            ├── view.go        # NEW
            ├── tabs.go        # NEW (if using tabs)
            └── help.go        # NEW
```

## Implementation Order

1. **Data Models** (`internal/db/models/table.go`)
   - Define Schema, Table, Index, TableColumn, Constraint structs
   - Add helper functions (FormatBytes, etc.)

2. **Database Queries** (`internal/db/queries/tables.go`)
   - GetSchemas
   - GetTablesWithStats
   - GetIndexesWithStats
   - GetPartitionHierarchy
   - GetTableDetails
   - CheckPgstattupleExtension
   - InstallPgstattupleExtension
   - GetTableBloat
   - ExecuteVacuum, ExecuteAnalyze, ExecuteReindex

3. **View Implementation** (`internal/ui/views/tables/view.go`)
   - Basic struct and Init/Update/View
   - Tree rendering with expand/collapse
   - Keyboard navigation
   - Sort functionality

4. **Help Overlay** (`internal/ui/views/tables/help.go`)
   - Keyboard shortcuts reference

5. **App Integration** (`internal/app/app.go`)
   - Register `5` key for Tables view
   - Add TablesView to view map

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run tables-specific tests
go test ./internal/db/queries/... -run Tables
go test ./internal/ui/views/tables/...
```

### Integration Tests

```bash
# Start test container
docker run -d --name steep-test -p 5433:5432 -e POSTGRES_PASSWORD=test postgres:16

# Run integration tests
go test ./internal/db/queries/... -tags=integration

# Clean up
docker stop steep-test && docker rm steep-test
```

### Manual Testing Checklist

- [ ] Press `5` opens Tables view
- [ ] Schemas display in collapsed state
- [ ] `Enter` expands/collapses schemas
- [ ] Tables show Size, Rows, Bloat %, Cache Hit %
- [ ] `j`/`k` navigation works
- [ ] `s` cycles sort columns
- [ ] `S` toggles sort direction
- [ ] `P` toggles system schemas
- [ ] `d` or `Enter` on table opens details
- [ ] Details shows columns, constraints, indexes
- [ ] `Esc` closes details panel
- [ ] `y` copies table name to clipboard
- [ ] `h` shows help overlay
- [ ] Partitioned tables show children when expanded
- [ ] pgstattuple install prompt appears if not installed
- [ ] `v`/`a`/`r` prompts for VACUUM/ANALYZE/REINDEX (non-readonly)
- [ ] Operations disabled in readonly mode
- [ ] 30-second auto-refresh works
- [ ] Unused indexes highlighted yellow
- [ ] High bloat (>20%) highlighted red

## Common Issues

### pgstattuple Not Available

If bloat shows "N/A", install the extension:

```sql
CREATE EXTENSION pgstattuple;
```

Or let Steep prompt you to install it (requires CREATE EXTENSION privilege).

### Permission Denied for Maintenance

VACUUM, ANALYZE, REINDEX require appropriate privileges:

```sql
-- Grant privileges if needed
GRANT ALL ON TABLE public.users TO your_user;
```

### Slow Queries

If queries take >500ms, check:
1. Database has been ANALYZED recently
2. pg_stat_* tables have data
3. No unusually large number of tables (>10,000)

## Reference Implementation

Study existing views for patterns:

- `internal/ui/views/locks/view.go` - Modal overlays, confirmation dialogs
- `internal/ui/views/queries/view.go` - Tab navigation, details panel
- `internal/db/queries/locks.go` - Query structure, error handling
