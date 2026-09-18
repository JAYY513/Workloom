// Package app is the shared application service behind every devsys surface
// (CLI, MCP, file protocol): one implementation of input validation, gates,
// approvals, version guards and transactions, so no entry point can bypass a
// constraint the others enforce (方案 §8.1, 实施计划 M4.2).
//
// Errors carry the taxonomy both surfaces render: Kind is the display kind
// the CLI already used (usage, precondition, invalid, workflow, gate, quality,
// workitem, approval), and Class collapses it onto the four classes that map
// to exit codes and MCP tool error codes.
package app

import (
	"errors"
	"fmt"
	"time"

	"workloom/internal/config"
	"workloom/internal/workitem"
)

// Error kinds. The first four are the shared classes; the rest are display
// kinds that all collapse onto "invalid".
const (
	KindUsage        = "usage"
	KindPrecondition = "precondition"
	KindInternal     = "internal"
	KindInvalid      = "invalid"
	KindWorkflow     = "workflow"
	KindGate         = "gate"
	KindQuality      = "quality"
	KindWorkitem     = "workitem"
	KindApproval     = "approval"
)

// Error is a classified application failure.
type Error struct {
	Kind     string
	Message  string
	Problems []config.Problem
}

func (e *Error) Error() string { return e.Message }

// Class collapses the display kind onto the four shared classes: usage,
// precondition, internal or invalid. The CLI maps classes to exit codes
// (2/3/1/4) and MCP to tool error codes (usage/precondition/internal/invalid).
func (e *Error) Class() string {
	switch e.Kind {
	case KindUsage, KindPrecondition, KindInternal:
		return e.Kind
	default:
		return KindInvalid
	}
}

// Usagef reports invalid input the schema could not express.
func Usagef(format string, a ...any) *Error {
	return &Error{Kind: KindUsage, Message: fmt.Sprintf(format, a...)}
}

// Preconditionf reports a state or environment precondition.
func Preconditionf(format string, a ...any) *Error {
	return &Error{Kind: KindPrecondition, Message: fmt.Sprintf(format, a...)}
}

// Internalf reports an unexpected failure.
func Internalf(format string, a ...any) *Error {
	return &Error{Kind: KindInternal, Message: fmt.Sprintf(format, a...)}
}

// Invalidf reports managed state that exists but cannot be trusted; kind is
// the display kind (invalid, workflow, gate, quality, workitem, approval).
func Invalidf(kind string, problems []config.Problem, format string, a ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, a...), Problems: problems}
}

// Classify wraps an arbitrary error as an application error. Errors that are
// already classified pass through unchanged.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return Internalf("%v", err)
}

// Service is the application service bound to one project root.
type Service struct {
	Root string
	// Now is the clock; nil means time.Now().UTC().
	Now func() time.Time
}

// New returns a service bound to root.
func New(root string) *Service { return &Service{Root: root} }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) items() *workitem.Store { return workitem.New(s.Root) }

// metadata loads the managed metadata files, classifying located problems.
func (s *Service) metadata() (*config.Metadata, error) {
	md, problems := config.Load(s.Root)
	if len(problems) > 0 {
		return nil, Invalidf(KindInvalid, problems, "invalid managed state (%d problems)", len(problems))
	}
	return md, nil
}

// project returns the project metadata or a precondition error when the
// project is not initialized.
func (s *Service) project() (*config.Metadata, error) {
	md, err := s.metadata()
	if err != nil {
		return nil, err
	}
	if md.Project == nil {
		return nil, Preconditionf("project not initialized; run devsys init")
	}
	return md, nil
}
