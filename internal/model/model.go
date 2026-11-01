// Package model contains transport-independent, shared domain values.
package model

import "time"

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type Account struct {
	User         User   `json:"user"`
	PasswordHash []byte `json:"password_hash"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// PlayerState is authoritative server state, never a copy of unchecked client input.
type PlayerState struct {
	PlayerID  string    `json:"player_id"`
	Position  Position  `json:"position"`
	Yaw       float64   `json:"yaw"`
	LastSeq   uint64    `json:"last_seq"`
	UpdatedAt time.Time `json:"updated_at"`
}
