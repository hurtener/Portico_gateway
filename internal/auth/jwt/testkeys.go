//go:build test_helpers || test
// +build test_helpers test

package jwt

// This file intentionally has a build tag so it doesn't ship in production
// binaries. testdata/jwks-test.json is the static fixture used by the
// validate command's smoke path.
