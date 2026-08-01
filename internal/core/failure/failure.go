package failure

import (
	"errors"
	"fmt"
)

// Refusal is the shared fail-closed error returned at trust boundaries.
type Refusal struct {
	Code   string
	Root   string
	Path   string
	Reason string
	Next   string
}

// New builds a refusal. Every refusal owes the caller one legal recovery
// action, so an empty code, reason, or next is a construction bug rather than a
// runtime condition: refusing without a way forward is the dead end the
// fail-closed contract exists to prevent. Panic keeps that undetectable in
// tests instead of shipping a dead end to an agent or a person.
func New(code, root, path, reason, next string) *Refusal {
	switch {
	case code == "":
		panic("failure.New: empty code")
	case reason == "":
		panic("failure.New: refusal " + code + " has no reason")
	case next == "":
		panic("failure.New: refusal " + code + " has no recovery action")
	}
	return &Refusal{Code: code, Root: root, Path: path, Reason: reason, Next: next}
}

func (r *Refusal) Error() string {
	if r.Path == "" {
		return fmt.Sprintf("%s: %s; next: %s", r.Code, r.Reason, r.Next)
	}
	return fmt.Sprintf("%s: %s: %s; next: %s", r.Code, r.Path, r.Reason, r.Next)
}

func IsCode(err error, code string) bool {
	var refusal *Refusal
	return errors.As(err, &refusal) && refusal.Code == code
}
