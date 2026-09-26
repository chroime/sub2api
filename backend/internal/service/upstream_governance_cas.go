package service

import "context"

type governanceMutationKey struct{}
type governanceMutation struct {
	expected string
	groups   []int64
}

// Only the local governance adapter can construct this context value.
func withGovernanceMutation(ctx context.Context, expected string, groups []int64) context.Context {
	return context.WithValue(ctx, governanceMutationKey{}, governanceMutation{expected: expected, groups: append([]int64(nil), groups...)})
}

// GovernanceMutationFromContext exposes the internal write precondition to persistence.
func GovernanceMutationFromContext(ctx context.Context) (string, []int64, bool) {
	v, ok := ctx.Value(governanceMutationKey{}).(governanceMutation)
	return v.expected, append([]int64(nil), v.groups...), ok
}

// GovernanceAccountFingerprint is shared with the locked repository CAS check.
func GovernanceAccountFingerprint(a *Account) string { return governanceLocal(a).Fingerprint }
