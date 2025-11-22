#!/bin/bash
# Test script for generating query activity for steep monitoring

set -e

# Read from steep config file
CONFIG_FILE="${HOME}/.config/steep/config.yaml"

if [ -f "$CONFIG_FILE" ]; then
    DB_HOST=$(grep "^  host:" "$CONFIG_FILE" | awk '{print $2}' | tr -d '"')
    DB_PORT=$(grep "^  port:" "$CONFIG_FILE" | awk '{print $2}' | tr -d '"')
    DB_NAME=$(grep "^  database:" "$CONFIG_FILE" | awk '{print $2}' | tr -d '"')
    DB_USER=$(grep "^  user:" "$CONFIG_FILE" | awk '{print $2}' | tr -d '"')
fi

# Fall back to environment variables or defaults
DB_HOST="${DB_HOST:-${PGHOST:-localhost}}"
DB_PORT="${DB_PORT:-${PGPORT:-5432}}"
DB_NAME="${DB_NAME:-${PGDATABASE:-postgres}}"
DB_USER="${DB_USER:-${PGUSER:-$USER}}"

PSQL="psql -h $DB_HOST -p $DB_PORT -d $DB_NAME -U $DB_USER"

usage() {
    echo "Usage: $0 {setup|run|cleanup|loop}"
    echo ""
    echo "  setup   - Create test table"
    echo "  run     - Run various test queries"
    echo "  cleanup - Drop test table"
    echo "  loop    - Continuous queries (for testing auto-refresh)"
}

setup() {
    echo "Creating test tables..."
    $PSQL <<EOF
-- Create test tables
CREATE TABLE IF NOT EXISTS steep_test_users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW(),
    score INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS steep_test_orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES steep_test_users(id),
    total DECIMAL(10,2),
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS steep_test_products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(200),
    price DECIMAL(10,2),
    category VARCHAR(100)
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_users_name ON steep_test_users(name);
CREATE INDEX IF NOT EXISTS idx_users_score ON steep_test_users(score);
CREATE INDEX IF NOT EXISTS idx_orders_user ON steep_test_orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON steep_test_orders(status);
CREATE INDEX IF NOT EXISTS idx_products_category ON steep_test_products(category);

-- Insert sample data
INSERT INTO steep_test_users (name, email, score)
SELECT
    'User ' || i,
    'user' || i || '@example.com',
    (random() * 1000)::int
FROM generate_series(1, 1000) AS i
ON CONFLICT DO NOTHING;

INSERT INTO steep_test_products (name, price, category)
SELECT
    'Product ' || i,
    (random() * 100 + 1)::decimal(10,2),
    CASE (i % 5)
        WHEN 0 THEN 'Electronics'
        WHEN 1 THEN 'Clothing'
        WHEN 2 THEN 'Books'
        WHEN 3 THEN 'Home'
        ELSE 'Sports'
    END
FROM generate_series(1, 500) AS i
ON CONFLICT DO NOTHING;

INSERT INTO steep_test_orders (user_id, total, status)
SELECT
    (random() * 999 + 1)::int,
    (random() * 500 + 10)::decimal(10,2),
    CASE (i % 4)
        WHEN 0 THEN 'pending'
        WHEN 1 THEN 'processing'
        WHEN 2 THEN 'shipped'
        ELSE 'delivered'
    END
FROM generate_series(1, 2000) AS i
ON CONFLICT DO NOTHING;

EOF
    echo "Test tables created with sample data."
}

run() {
    echo "Running test queries..."

    # Simple selects with different row counts
    echo "  - Simple SELECT queries..."
    $PSQL -c "SELECT * FROM steep_test_users WHERE id = 1;" > /dev/null
    $PSQL -c "SELECT * FROM steep_test_users WHERE score > 500;" > /dev/null
    $PSQL -c "SELECT * FROM steep_test_users WHERE name LIKE 'User 1%';" > /dev/null
    $PSQL -c "SELECT COUNT(*) FROM steep_test_users;" > /dev/null

    # Aggregations
    echo "  - Aggregation queries..."
    $PSQL -c "SELECT status, COUNT(*), AVG(total) FROM steep_test_orders GROUP BY status;" > /dev/null
    $PSQL -c "SELECT category, COUNT(*), MIN(price), MAX(price) FROM steep_test_products GROUP BY category;" > /dev/null

    # Joins
    echo "  - JOIN queries..."
    $PSQL -c "SELECT u.name, o.total FROM steep_test_users u JOIN steep_test_orders o ON u.id = o.user_id LIMIT 100;" > /dev/null
    $PSQL -c "SELECT u.name, COUNT(o.id) as order_count FROM steep_test_users u LEFT JOIN steep_test_orders o ON u.id = o.user_id GROUP BY u.id, u.name HAVING COUNT(o.id) > 2 LIMIT 50;" > /dev/null

    # Subqueries
    echo "  - Subquery queries..."
    $PSQL -c "SELECT * FROM steep_test_users WHERE id IN (SELECT user_id FROM steep_test_orders WHERE total > 300) LIMIT 50;" > /dev/null
    $PSQL -c "SELECT * FROM steep_test_products WHERE price > (SELECT AVG(price) FROM steep_test_products);" > /dev/null

    # Updates
    echo "  - UPDATE queries..."
    $PSQL -c "UPDATE steep_test_users SET score = score + 1 WHERE id <= 10;" > /dev/null
    $PSQL -c "UPDATE steep_test_orders SET status = 'processing' WHERE status = 'pending' AND id <= 5;" > /dev/null

    # Inserts
    echo "  - INSERT queries..."
    $PSQL -c "INSERT INTO steep_test_users (name, email, score) VALUES ('Test User', 'test@example.com', 100) ON CONFLICT DO NOTHING;" > /dev/null

    # Deletes
    echo "  - DELETE queries..."
    $PSQL -c "DELETE FROM steep_test_orders WHERE id > 2000;" > /dev/null

    # Complex queries
    echo "  - Complex queries..."
    $PSQL -c "WITH top_users AS (SELECT id, name, score FROM steep_test_users ORDER BY score DESC LIMIT 10) SELECT t.*, COUNT(o.id) FROM top_users t LEFT JOIN steep_test_orders o ON t.id = o.user_id GROUP BY t.id, t.name, t.score;" > /dev/null

    echo "Test queries completed."
}

loop() {
    echo "Running continuous queries (Ctrl+C to stop)..."
    echo ""

    i=0
    while true; do
        i=$((i + 1))

        # Mix of fast and slow queries
        case $((i % 10)) in
            0)
                # Slow query - sequential scan
                $PSQL -c "SELECT * FROM steep_test_users WHERE name LIKE '%5%';" > /dev/null 2>&1
                echo "  [$i] Slow pattern match query"
                ;;
            1)
                # Fast indexed lookup
                $PSQL -c "SELECT * FROM steep_test_users WHERE id = $((RANDOM % 1000 + 1));" > /dev/null 2>&1
                echo "  [$i] Fast indexed lookup"
                ;;
            2)
                # Aggregation
                $PSQL -c "SELECT status, COUNT(*) FROM steep_test_orders GROUP BY status;" > /dev/null 2>&1
                echo "  [$i] Aggregation query"
                ;;
            3)
                # Join query
                $PSQL -c "SELECT u.name, o.total FROM steep_test_users u JOIN steep_test_orders o ON u.id = o.user_id WHERE o.total > $((RANDOM % 200 + 100)) LIMIT 20;" > /dev/null 2>&1
                echo "  [$i] Join query"
                ;;
            4)
                # Update
                $PSQL -c "UPDATE steep_test_users SET score = score + 1 WHERE id = $((RANDOM % 100 + 1));" > /dev/null 2>&1
                echo "  [$i] Update query"
                ;;
            5)
                # Insert
                $PSQL -c "INSERT INTO steep_test_orders (user_id, total, status) VALUES ($((RANDOM % 1000 + 1)), $((RANDOM % 500 + 10)), 'pending');" > /dev/null 2>&1
                echo "  [$i] Insert query"
                ;;
            6)
                # Count with condition
                $PSQL -c "SELECT COUNT(*) FROM steep_test_orders WHERE total > $((RANDOM % 300 + 50));" > /dev/null 2>&1
                echo "  [$i] Count query"
                ;;
            7)
                # Subquery
                $PSQL -c "SELECT * FROM steep_test_users WHERE score > (SELECT AVG(score) FROM steep_test_users) LIMIT 10;" > /dev/null 2>&1
                echo "  [$i] Subquery"
                ;;
            8)
                # Multiple conditions
                $PSQL -c "SELECT * FROM steep_test_products WHERE category = 'Electronics' AND price < $((RANDOM % 50 + 20));" > /dev/null 2>&1
                echo "  [$i] Filtered select"
                ;;
            9)
                # Delete old orders (cycles through)
                $PSQL -c "DELETE FROM steep_test_orders WHERE id IN (SELECT id FROM steep_test_orders ORDER BY id DESC LIMIT 1);" > /dev/null 2>&1
                echo "  [$i] Delete query"
                ;;
        esac

        # Random delay between 0.1 and 1 second
        sleep 0.$((RANDOM % 9 + 1))
    done
}

cleanup() {
    echo "Cleaning up test tables..."
    $PSQL <<EOF
DROP TABLE IF EXISTS steep_test_orders CASCADE;
DROP TABLE IF EXISTS steep_test_users CASCADE;
DROP TABLE IF EXISTS steep_test_products CASCADE;
EOF
    echo "Test tables dropped."
}

case "$1" in
    setup)
        setup
        ;;
    run)
        run
        ;;
    cleanup)
        cleanup
        ;;
    loop)
        loop
        ;;
    *)
        usage
        exit 1
        ;;
esac
