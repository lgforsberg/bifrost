package mail

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrAuthFailed       = errors.New("authentication failed")
	ErrConnectionFailed = errors.New("connection failed")
	ErrSendRejected     = errors.New("send rejected")
	ErrInvalidConfig    = errors.New("invalid configuration")
)
