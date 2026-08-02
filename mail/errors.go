package mail

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrAuthFailed       = errors.New("authentication failed")
	ErrConnectionFailed = errors.New("connection failed")
	ErrSendRejected     = errors.New("send rejected")
	ErrInvalidConfig    = errors.New("invalid configuration")

	// ErrPendingApproval reports a draft that is still waiting for someone to
	// approve it. Distinct from the other failures because nothing is broken:
	// the caller is being told to get approval, or to say explicitly that it
	// is not needed.
	ErrPendingApproval = errors.New("pending approval")
)
