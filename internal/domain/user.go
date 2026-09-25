package domain

import "errors"

// User is someone who can sign in to the platform.
type User struct {
	ID    string
	Email string
	Name  string
	// PasswordHash is a bcrypt hash and never leaves the server.
	PasswordHash string
}

// ErrInvalidCredentials is returned for a wrong email or password, without
// saying which one was wrong.
var ErrInvalidCredentials = errors.New("invalid email or password")

// ErrUnauthorized is returned when a session token is missing, invalid or expired.
var ErrUnauthorized = errors.New("unauthorized")
