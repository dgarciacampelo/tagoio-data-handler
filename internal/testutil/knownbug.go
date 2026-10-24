// Package testutil holds helpers shared by the tests of several packages: the known-bug marker and an in-memory
// fake of the TagoIO REST API. Only test files import it.
package testutil

import "testing"

// KnownBug ends a test of a bug that is not fixed yet. The test checks the correct behaviour itself and passes the
// outcome as ok, after any setup assertions (a broken setup must still fail with t.Fatal):
//
//   - while the bug is present (ok is false), the test is skipped with "known bug: <reason>";
//   - once the bug is fixed (ok is true), the test fails, so the KnownBug call is replaced by a plain assertion in
//     the same change as the fix.
//
// It is the Go counterpart of the strict xfail markers of the neos-ocpp test suite.
func KnownBug(t testing.TB, ok bool, reason string) {
	t.Helper()
	if ok {
		t.Fatalf("known bug looks fixed, replace KnownBug with a plain assertion: %s", reason)
	}
	t.Skipf("known bug: %s", reason)
}
