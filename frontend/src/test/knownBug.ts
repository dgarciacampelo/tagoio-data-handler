// Known-bug marker of the frontend tests: the counterpart of testutil.KnownBug (Go) and of the strict xfail markers
// of the Python suites.
//
// A test of an open bug asserts the CORRECT behaviour inside knownBug():
//   - while the bug is present, the assertion fails and the test is skipped with "known bug: <reason>";
//   - once the bug is fixed, the assertion passes and the test FAILS, so the knownBug() call is replaced by the plain
//     assertion in the same change as the fix.
//
// Only assertion errors (expect/assert) mean "the bug is still present". Any other error (a component that does not
// mount, a missing import, a TypeError) fails the test as usual. Keep the setup and its sanity checks outside the
// callback, so that a broken setup cannot pass for the bug.

import { chai, type TestContext } from 'vitest';

export async function knownBug(
	context: TestContext,
	reason: string,
	assertion: () => void | Promise<void>
): Promise<void> {
	try {
		await assertion();
	} catch (error) {
		if (error instanceof chai.AssertionError) {
			context.skip(`known bug: ${reason}`);
		}
		throw error;
	}
	throw new Error(`known bug looks fixed, replace knownBug() with the plain assertion: ${reason}`);
}
