package service

import "context"

type governanceMutationKey struct{}
type governanceMutation struct {
	expected string
	groups   []int64
	targets  map[int64]string
}

// Only the local governance adapter can construct this context value.
func withGovernanceMutation(ctx context.Context, expected string, groups []int64, targets ...map[int64]string) context.Context {
	value := governanceMutation{expected: expected, groups: append([]int64(nil), groups...)}
	if len(targets) > 0 {
		value.targets = make(map[int64]string, len(targets[0]))
		for id, fingerprint := range targets[0] {
			value.targets[id] = fingerprint
		}
	}
	return context.WithValue(ctx, governanceMutationKey{}, value)
}

func GovernanceTargetsFromContext(ctx context.Context) map[int64]string {
	v, _ := ctx.Value(governanceMutationKey{}).(governanceMutation)
	targets := make(map[int64]string, len(v.targets))
	for id, fingerprint := range v.targets {
		targets[id] = fingerprint
	}
	return targets
}

// GovernanceMutationFromContext exposes the internal write precondition to persistence.
func GovernanceMutationFromContext(ctx context.Context) (string, []int64, bool) {
	v, ok := ctx.Value(governanceMutationKey{}).(governanceMutation)
	return v.expected, append([]int64(nil), v.groups...), ok
}

// GovernanceAccountFingerprint is shared with the locked repository CAS check.
func GovernanceAccountFingerprint(a *Account) string { return governanceLocal(a).Fingerprint }
