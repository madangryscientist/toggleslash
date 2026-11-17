import { expect, test, type APIRequestContext } from "@playwright/test";
import { BUILD_COMMIT_PATTERN } from "../src/build-commit.ts";

/*
 * After a deploy, against the live host (SMOKE_BASE_URL): the home page names the commit it was built from
 * in its build-commit meta. Request only, no browser. The playwright config gives this file its own project,
 * and `test:e2e` never runs it.
 */

const EXPECT_COMMIT = process.env["SMOKE_EXPECT_COMMIT"];
/** How long an edge may keep serving the previous page after the upload before the check fails. */
const COMMIT_WAIT_MS = 120_000;

interface HomePage {
  status: number;
  seen: string | undefined;
}

const buildCommitOf = (html: string): string | undefined =>
  /<meta name="build-commit" content="([^"]*)"/.exec(html)?.[1];

async function homePage(request: APIRequestContext): Promise<HomePage> {
  // A fresh query each time, so no cache answers with an older copy of the page.
  const response = await request.get(`/?commit-check=${crypto.randomUUID()}`, { timeout: 15_000 });
  return {
    status: response.status(),
    seen: response.ok() ? buildCommitOf(await response.text()) : undefined,
  };
}

/** A dropped connection while the edge switches over counts as not yet, unless this is the last try. */
async function homePageOrNotYet(request: APIRequestContext, last: boolean): Promise<HomePage> {
  try {
    return await homePage(request);
  } catch (error) {
    if (last) throw error;
    return { status: 0, seen: undefined };
  }
}

test("the home page names the commit it was built from", async ({ request }) => {
  test.setTimeout(COMMIT_WAIT_MS + 30_000);
  const deadline = Date.now() + (EXPECT_COMMIT ? COMMIT_WAIT_MS : 0);
  let page = await homePageOrNotYet(request, !EXPECT_COMMIT);
  // With no EXPECT_COMMIT the deadline is now, so the loop does not run.
  while (page.seen !== EXPECT_COMMIT && Date.now() < deadline) {
    await new Promise((done) => setTimeout(done, 5_000));
    page = await homePageOrNotYet(request, Date.now() >= deadline);
  }
  expect(page.status).toBe(200);
  expect(page.seen, "the home page has no build-commit meta").toBeDefined();
  expect(page.seen).toMatch(BUILD_COMMIT_PATTERN);
  if (EXPECT_COMMIT)
    expect(
      page.seen,
      `the live home page names ${page.seen}, not the deployed ${EXPECT_COMMIT}`,
    ).toBe(EXPECT_COMMIT);
});
