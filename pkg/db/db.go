// Package db provides public access to the ctx memory store.
// This package re-exports types and functions from the internal implementation
// to allow external consumers to embed the store.
package db

import "github.com/zate/ctx/internal/db"

// Type aliases for external consumers.
type (
	Store           = db.Store
	Node            = db.Node
	Edge            = db.Edge
	CreateNodeInput = db.CreateNodeInput
	UpdateNodeInput = db.UpdateNodeInput
	ListOptions     = db.ListOptions
)

// Open opens a SQLite database at the given path.
// Creates the database and runs migrations if it doesn't exist.
func Open(path string) (*db.SQLiteStore, error) {
	return db.Open(path)
}
