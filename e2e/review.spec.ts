import { randomUUID } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  configure,
  disconnectAll,
  fakeState,
  resetFakes,
  signIn,
} from "./helpers";

// Review actions: approve, request changes and mark ready, with the approvals required per
// repository. No screen has these buttons yet, so the spec calls the same endpoints the cards
// will, from the page, with the session cookie and the CSRF header, and checks what the fake
// GitHub received and how the snapshot changed. The fake adds three pull requests in acme/review
// (config review): #31 has one approval, #32 is a draft and #33 was opened by the viewer.

type GitHubState = {
  merges: number;
  nodeQueries: number;
  reviewCalls: number;
  readyCalls: number;
  // null until the first one: the fake leaves empty lists unset.
  reviews: { pr: string; event: string; sha: string; body: string }[] | null;
  ready: string[] | null;
};
type SnapshotPr = {
  id: string;
  number: number;
  isDraft: boolean;
  reviewDecision: string | null;
  approvals: { have: number; need: number; changesRequested: number };
  repo: { id: string; nameWithOwner: string };
};
type Reply = { status: number; body: Record<string, unknown> | null };

const REPO = "R_acme_review";
const headOf = (number: number) =>
  (number * 7919).toString(16).padStart(40, "0");
const idOf = (number: number) => `PR_acme_review_${number}`;

// A request from inside the page: the cookie travels by itself, the CSRF token is read from the
// session the way the web client does.
async function call(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<Reply> {
  return page.evaluate(
    async (a) => {
      const s = (await (
        await fetch("/api/session", { credentials: "same-origin" })
      ).json()) as { csrfToken: string };
      const res = await fetch(a.path, {
        method: a.method,
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          "X-PRD-CSRF": s.csrfToken,
          ...a.headers,
        },
        body: a.body === undefined ? undefined : JSON.stringify(a.body),
      });
      return {
        status: res.status,
        body: (await res.json().catch(() => null)) as Record<
          string,
          unknown
        > | null,
      };
    },
    { method, path, body, headers },
  );
}

const snapshotPr = async (page: Page, number: number) => {
  const r = await page.evaluate(async () => {
    const res = await fetch("/api/snapshot", { credentials: "same-origin" });
    return (await res.json()) as { prs: SnapshotPr[] };
  });
  return r.prs.find(
    (p) => p.repo.nameWithOwner === "acme/review" && p.number === number,
  );
};

const setApprovals = (page: Page, n: number) =>
  call(page, "PUT", `/api/repos/${REPO}`, {
    defaultMethod: "squash",
    branchRules: [],
    clearNew: false,
    approvalsRequired: n,
  });

const mergeRequest = (number: number) => ({
  method: "squash",
  expectedHeadSha: headOf(number),
  deleteBranch: false,
  tickets: [],
});

test.beforeAll(async ({ browser, request }) => {
  const page = await (await browser.newContext()).newPage();
  await resetFakes(request);
  await signIn(page, "review");
  await disconnectAll(page);
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();
  await page.context().close();
});

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await configure(request, "github", { review: true });
  await signIn(page, "review");
  await page.goto(APP + "/");
  // Writes are limited to 30 a minute per session: sync only while the fixtures are not in yet.
  await expect(async () => {
    if (!(await snapshotPr(page, 31))) {
      expect((await call(page, "POST", "/api/sync")).status).toBe(202);
    }
    await expect
      .poll(async () => (await snapshotPr(page, 31))?.number, { timeout: 4000 })
      .toBe(31);
  }).toPass({ intervals: [500, 1500], timeout: 15_000 });
});

const pollSnapshot = (
  page: Page,
  number: number,
  read: (p: SnapshotPr) => unknown,
) =>
  expect.poll(
    async () => {
      const p = await snapshotPr(page, number);
      return p && read(p);
    },
    { timeout: 10_000 },
  );

test("a repository that needs two approvals blocks the merge until the second one", async ({
  page,
  request,
}) => {
  // The setting is stored with the app, not with the fakes: start from the default.
  expect((await setApprovals(page, 1)).status).toBe(200);
  expect((await snapshotPr(page, 31))?.approvals).toEqual({
    have: 1,
    need: 1,
    changesRequested: 0,
  });
  const saved = await setApprovals(page, 2);
  expect(saved.status).toBe(200);
  expect(saved.body).toMatchObject({ id: REPO, approvalsRequired: 2 });
  // Saving rebuilds the snapshot from cached data, so the count follows at once.
  expect((await snapshotPr(page, 31))?.approvals).toEqual({
    have: 1,
    need: 2,
    changesRequested: 0,
  });

  // The server refuses the merge with the same words the card shows.
  const refused = await call(
    page,
    "POST",
    `/api/prs/${idOf(31)}/merge`,
    mergeRequest(31),
    {
      "Idempotency-Key": randomUUID(),
    },
  );
  expect(refused.status).toBe(409);
  expect(refused.body).toMatchObject({
    code: "not_mergeable",
    detail: "Needs 2 approvals, has 1",
  });
  expect((await fakeState<GitHubState>(request, "github")).merges).toBe(0);

  const approved = await call(page, "POST", `/api/prs/${idOf(31)}/approve`, {
    expectedHeadSha: headOf(31),
    body: "ship it",
  });
  expect(approved.status).toBe(200);
  expect(approved.body).toEqual({ prId: idOf(31), action: "approve" });
  const github = await fakeState<GitHubState>(request, "github");
  expect(github.reviews).toEqual([
    { pr: idOf(31), event: "APPROVE", sha: headOf(31), body: "ship it" },
  ]);
  // The server re-checked the pull request with one node query before writing.
  expect(github.nodeQueries).toBeGreaterThanOrEqual(1);

  // One refresh follows the write: the snapshot shows the second approval.
  await pollSnapshot(page, 31, (p) => p.approvals).toEqual({
    have: 2,
    need: 2,
    changesRequested: 0,
  });

  const merged = await call(
    page,
    "POST",
    `/api/prs/${idOf(31)}/merge`,
    mergeRequest(31),
    {
      "Idempotency-Key": randomUUID(),
    },
  );
  expect(merged.status).toBe(200);
  expect((await fakeState<GitHubState>(request, "github")).merges).toBe(1);
});

test("requests changes with a required text, and checks the head first", async ({
  page,
  request,
}) => {
  const url = `/api/prs/${idOf(31)}/request-changes`;
  expect(
    (await call(page, "POST", url, { expectedHeadSha: headOf(31) })).status,
  ).toBe(400);
  expect(
    (await call(page, "POST", url, { expectedHeadSha: headOf(31), body: "" }))
      .status,
  ).toBe(400);
  const blank = await call(page, "POST", url, {
    expectedHeadSha: headOf(31),
    body: "   ",
  });
  expect(blank.status).toBe(422);
  expect(blank.body).toMatchObject({ code: "body_required" });

  // A head that is not the one the page saw: refused with the fresh state, nothing written.
  const stale = await call(page, "POST", url, {
    expectedHeadSha: headOf(99),
    body: "rename it",
  });
  expect(stale.status).toBe(409);
  expect(stale.body).toMatchObject({
    code: "head_moved",
    pr: { state: "OPEN", headSha: headOf(31), isDraft: false },
  });
  expect(
    (await fakeState<GitHubState>(request, "github")).reviews ?? [],
  ).toEqual([]);

  const done = await call(page, "POST", url, {
    expectedHeadSha: headOf(31),
    body: "rename it",
  });
  expect(done.status).toBe(200);
  expect(done.body).toEqual({ prId: idOf(31), action: "request_changes" });
  expect((await fakeState<GitHubState>(request, "github")).reviews).toEqual([
    {
      pr: idOf(31),
      event: "REQUEST_CHANGES",
      sha: headOf(31),
      body: "rename it",
    },
  ]);
  await pollSnapshot(page, 31, (p) => [
    p.reviewDecision,
    p.approvals.changesRequested,
  ]).toEqual(["CHANGES_REQUESTED", 1]);
});

test("marks a draft ready for review once", async ({ page, request }) => {
  expect((await snapshotPr(page, 32))?.isDraft).toBe(true);
  const url = `/api/prs/${idOf(32)}/ready`;
  const stale = await call(page, "POST", url, { expectedHeadSha: headOf(1) });
  expect(stale.status).toBe(409);
  expect(stale.body).toMatchObject({
    code: "head_moved",
    pr: { isDraft: true },
  });
  expect((await fakeState<GitHubState>(request, "github")).readyCalls).toBe(0);

  const done = await call(page, "POST", url, { expectedHeadSha: headOf(32) });
  expect(done.status).toBe(200);
  expect(done.body).toEqual({ prId: idOf(32), action: "ready" });
  expect((await fakeState<GitHubState>(request, "github")).ready).toEqual([
    idOf(32),
  ]);
  await pollSnapshot(page, 32, (p) => p.isDraft).toBe(false);

  // It is not a draft any more, and #31 never was: refused from fresh data.
  for (const number of [32, 31]) {
    const again = await call(page, "POST", `/api/prs/${idOf(number)}/ready`, {
      expectedHeadSha: headOf(number),
    });
    expect(again.status).toBe(409);
    expect(again.body).toMatchObject({ code: "not_draft" });
  }
  expect((await fakeState<GitHubState>(request, "github")).readyCalls).toBe(1);
});

test("GitHub does not let the viewer review their own pull request", async ({
  page,
  request,
}) => {
  for (const [path, extra] of [
    ["approve", {}],
    ["request-changes", { body: "no" }],
  ] as const) {
    const r = await call(page, "POST", `/api/prs/${idOf(33)}/${path}`, {
      expectedHeadSha: headOf(33),
      ...extra,
    });
    expect(r.status).toBe(409);
    expect(r.body).toMatchObject({ code: "own_pull_request" });
  }
  expect(
    (await fakeState<GitHubState>(request, "github")).reviews ?? [],
  ).toEqual([]);
  // The author may take their own draft out of draft: ready ignores who opened it.
});

test("a token without Pull requests write is told which permission to add", async ({
  page,
  request,
}) => {
  const useToken = async (token: string) => {
    await page.goto(APP + "/setup/");
    await page.getByLabel("Personal access token").fill(token);
    await page.getByRole("button", { name: "Validate and save" }).click();
    await expect(page.getByText("Connected as octocat")).toBeVisible();
  };
  await useToken("github_pat_pulls_read");
  try {
    await page.goto(APP + "/");
    const review = await call(page, "POST", `/api/prs/${idOf(31)}/approve`, {
      expectedHeadSha: headOf(31),
    });
    expect(review.status).toBe(403);
    expect(review.body).toMatchObject({
      code: "missing_permission",
      detail: "The token needs the GitHub permission pull_requests: write",
    });
    const ready = await call(page, "POST", `/api/prs/${idOf(32)}/ready`, {
      expectedHeadSha: headOf(32),
    });
    expect(ready.status).toBe(403);
    expect(ready.body).toMatchObject({
      code: "missing_permission",
      detail: "The token needs the GitHub permission pull_requests: write",
    });
    const github = await fakeState<GitHubState>(request, "github");
    expect(github.reviews ?? []).toEqual([]);
    expect(github.ready ?? []).toEqual([]);
  } finally {
    await useToken("ghp_classic_ok");
  }
});

test("the write endpoints keep the merge's guards", async ({ page }) => {
  const body = { expectedHeadSha: headOf(31) };
  // No CSRF header: refused before the handler.
  const noCsrf = await page.evaluate(async (b) => {
    const res = await fetch("/api/prs/PR_acme_review_31/approve", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(b),
    });
    return res.status;
  }, body);
  expect(noCsrf).toBe(403);
  const unknown = await call(page, "POST", "/api/prs/PR_nope/approve", body);
  expect(unknown.status).toBe(404);
  expect(unknown.body).toMatchObject({ code: "not_found" });
});
