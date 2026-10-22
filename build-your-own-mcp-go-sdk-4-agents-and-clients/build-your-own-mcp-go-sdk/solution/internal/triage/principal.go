package triage

import "context"

// Caller is who a request comes from: a principal named by a bearer token over
// HTTP, or the one local caller over stdio.
type Caller struct {
	Name     string
	ReadOnly bool
}

type callerKey struct{}

// WithCaller returns a context that carries caller to the tool and resource
// handlers.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// callerOf returns the caller ctx carries, or the local caller when none does.
func callerOf(ctx context.Context) Caller {
	if caller, found := ctx.Value(callerKey{}).(Caller); found {
		return caller
	}
	return Caller{Name: localCaller}
}
