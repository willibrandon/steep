// Test program for parameter logging
// Run: go run scripts/test_params.go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, "postgres://brandon@localhost:5432/brandon")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)

	fmt.Println("Running parameterized queries...")

	// These use extended query protocol with bound parameters
	var count int

	// Query with int parameter
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM steep_test_users WHERE score > $1", 500).Scan(&count)
	if err != nil {
		log.Printf("Query 1 error: %v", err)
	} else {
		fmt.Printf("  Users with score > 500: %d\n", count)
	}

	// Query with string parameter
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM steep_test_users WHERE name LIKE $1", "User 1%").Scan(&count)
	if err != nil {
		log.Printf("Query 2 error: %v", err)
	} else {
		fmt.Printf("  Users matching 'User 1%%': %d\n", count)
	}

	// Query with multiple parameters
	var total float64
	err = conn.QueryRow(ctx, "SELECT COALESCE(SUM(total), 0) FROM steep_test_orders WHERE status = $1 AND total > $2", "pending", 100.0).Scan(&total)
	if err != nil {
		log.Printf("Query 3 error: %v", err)
	} else {
		fmt.Printf("  Sum of pending orders > 100: %.2f\n", total)
	}

	// Update with parameters
	tag, err := conn.Exec(ctx, "UPDATE steep_test_users SET score = score + $1 WHERE id = $2", 10, 1)
	if err != nil {
		log.Printf("Update error: %v", err)
	} else {
		fmt.Printf("  Updated %d row(s)\n", tag.RowsAffected())
	}

	fmt.Println("Done. Check PostgreSQL logs for DETAIL lines with parameters.")
}
