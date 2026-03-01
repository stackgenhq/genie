package toolcontext

import "context"

type justificationKey struct{}

func WithJustification(ctx context.Context, justification string) context.Context {
	return context.WithValue(ctx, justificationKey{}, justification)
}

func GetJustification(ctx context.Context) string {
	justification, ok := ctx.Value(justificationKey{}).(string)
	if !ok {
		return ""
	}
	return justification
}
