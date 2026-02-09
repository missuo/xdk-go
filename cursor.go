package xdk

import (
	"context"
	"errors"
)

// PaginationError is returned when pagination cannot continue.
type PaginationError struct {
	Err error
}

func (e *PaginationError) Error() string {
	if e == nil || e.Err == nil {
		return "pagination error"
	}
	return e.Err.Error()
}

func (e *PaginationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Cursor is a thin wrapper around Pager.
type Cursor struct {
	pager *Pager
}

func NewCursor(pager *Pager) (*Cursor, error) {
	if pager == nil {
		return nil, &PaginationError{Err: errors.New("nil pager")}
	}
	return &Cursor{pager: pager}, nil
}

// CursorFor creates a cursor from a generated paginated method call result.
func CursorFor(pager *Pager) (*Cursor, error) {
	return NewCursor(pager)
}

func (c *Cursor) NextPage(ctx context.Context) (JSON, bool, error) {
	if c == nil || c.pager == nil {
		return nil, false, &PaginationError{Err: errors.New("cursor is nil")}
	}
	page, ok, err := c.pager.Next(ctx)
	if err != nil {
		return nil, ok, &PaginationError{Err: err}
	}
	return page, ok, nil
}
