package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Args[1])
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `
		SELECT u.email, r.name
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		WHERE u.email = 'admin@skolara.test'`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var email, role *string
		rows.Scan(&email, &role)
		fmt.Printf("user=%s role=%v\n", *email, role)
	}
	var n int
	conn.QueryRow(ctx, `SELECT count(*) FROM schools`).Scan(&n)
	fmt.Println("schools:", n)
	conn.QueryRow(ctx, `SELECT count(*) FROM school_memberships`).Scan(&n)
	fmt.Println("memberships:", n)
}
