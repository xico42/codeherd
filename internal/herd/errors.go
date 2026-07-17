package herd

import (
	"errors"
	"fmt"
)

// Sentinels. These are the whole error vocabulary of the domain; front ends
// match these and nothing else. They span three packages today, which is why
// cmd/errors.go grew two translators that both handle ErrNotCloned and print
// different text for it.
var (
	ErrNotCloned         = errors.New("project not cloned")
	ErrAlreadyCloned     = errors.New("already cloned")
	ErrWorktreeExists    = errors.New("worktree already exists")
	ErrWorktreeNotFound  = errors.New("worktree not found")
	ErrLocalBranchExists = errors.New("local branch already exists")
	ErrSessionExists     = errors.New("session already exists")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionRunning    = errors.New("session is running")
	ErrPathNotFound      = errors.New("worktree path not found")
)

// AlreadyClonedError carries the path that already exists.
type AlreadyClonedError struct{ Path string }

func (e *AlreadyClonedError) Error() string { return e.Path + " already exists, skipping" }
func (e *AlreadyClonedError) Unwrap() error { return ErrAlreadyCloned }

// SessionExistsError is returned by Launch when a session for the same Ref
// and type is already running. It carries the Ref so a front end can print an
// attach hint without re-deriving identity.
type SessionExistsError struct {
	Ref  Ref
	Type SessionType
}

func (e *SessionExistsError) Error() string {
	return fmt.Sprintf("%s: %s/%s (%s)", ErrSessionExists.Error(), e.Ref.Project, e.Ref.Branch, e.Type)
}

func (e *SessionExistsError) Unwrap() error { return ErrSessionExists }
