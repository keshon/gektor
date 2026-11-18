package workflow

import (
	"context"
	"encoding/json"
	"fmt"
)

// CallAuthorizer checks destinations discovered during execution through the service's current policy.
type CallAuthorizer interface {
	AuthorizeCall(context.Context, string, json.RawMessage) error
}

type callAuthorizerKey struct{}

func WithCallAuthorizer(ctx context.Context, authorizer CallAuthorizer) context.Context {
	return context.WithValue(ctx, callAuthorizerKey{}, authorizer)
}

func AuthorizeCall(ctx context.Context, name string, args json.RawMessage) error {
	authorizer, ok := ctx.Value(callAuthorizerKey{}).(CallAuthorizer)
	if !ok {
		return fmt.Errorf("dynamic destinations require an active workflow authorizer")
	}
	return authorizer.AuthorizeCall(ctx, name, args)
}
