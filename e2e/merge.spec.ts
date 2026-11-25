import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  configure,
  disconnectAll,
  fakeState,
  resetFakes,
  signIn,
} from "./helpers";

// TECH_SPEC section 12, flows 5 and 6: merge with a Jira move, a failed move that is retried, and
// a head that moved between loading the page and pressing Merge.

type GitHubState = {
  merges: number;
  branchDeletes: number;
  nodeQueries: number;
  searches: number;
  lastMerge: { method: string; sha: string; repo: string; number: string };
};
type JiraState = {
  statuses: Record<string, string>;
  moves: string[];
  jira: { moves: number; transitions: number };
};

const headOf = (number: number, moved = false) =>
  (number * 7919 + (moved ? 1 : 0)).toString(16).padStart(40, "0");

// The spec has its own session, because the write limit is per session and the other specs have
// used up the shared one. The accounts are connected once and each test only resets the fakes and
// syncs.
test.beforeAll(async ({ browser, request }) => {
  const page = await (await browser.newContext()).newPage();
  await resetFakes(request);
  await signIn(page, "merge");
  await disconnectAll(page);
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();
  await page.goto(APP + "/accounts/");
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.context().close();
});

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page, "merge");
  await page.goto(APP + "/");
  await expect(async () => {
    await page.getByRole("button", { name: "Sync", exact: true }).click();
    await expect(rowOf(page, "INP-104 Invoice export")).toBeVisible({
      timeout: 3000,
    });
  }).toPass({ intervals: [1000, 2000, 3000], timeout: 15_000 });
  await expect(rowOf(page, "Bump dependencies")).toBeVisible();
});

const rowOf = (page: Page, title: string) =>
  page.locator("tr[data-pr]").filter({ hasText: title });

test("merges and moves the ticket, with the required field", async ({
  page,
  request,
}) => {
  const row = rowOf(page, "INP-104 Invoice export");
  await expect(row).toBeVisible();
  await expect(row.getByRole("combobox", { name: "Merge method" })).toHaveText(
    "Merge commit (default)",
  );
  await expect(row.getByText("Branch rule main")).toBeVisible();
  await row.getByRole("button", { name: "Merge", exact: true }).click();

  const panel = page.getByRole("group", { name: "Merge #5" });
  await expect(panel.getByText("Don't move")).toBeVisible();
  await panel.getByRole("button", { name: "Move INP-104" }).click();
  await panel.getByRole("combobox", { name: "Move INP-104" }).click();
  await page.getByRole("option", { name: "Move to Done" }).click();

  const merge = panel.getByRole("button", { name: "Merge & move to Done" });
  await expect(merge).toBeDisabled();
  await expect(
    panel.getByText("INP-104: Resolution is required"),
  ).toBeVisible();
  await panel.getByLabel("Resolution (required)").selectOption("10000");
  await panel
    .getByRole("checkbox", { name: "Confirm moving INP-104 to Done" })
    .check();
  await expect(merge).toBeEnabled();
  await merge.click();

  const recent = page.getByRole("region", { name: "Recent merges" });
  await expect(recent.getByText("Move INP-104 to Done")).toBeVisible();
  await expect(recent.getByText("Failed", { exact: true })).toHaveCount(0);
  await expect(rowOf(page, "INP-104 Invoice export")).toHaveCount(0);

  const github = await fakeState<GitHubState>(request, "github");
  expect(github.merges).toBe(1);
  expect(github.nodeQueries).toBe(1);
  expect(github.branchDeletes).toBe(0);
  expect(github.lastMerge).toMatchObject({
    method: "merge",
    sha: headOf(5),
    repo: "acme-corp/billing",
    number: "5",
  });
  const jira = await fakeState<JiraState>(request, "atlassian");
  expect(jira.moves).toEqual(['INP-104:31:{"resolution":{"id":"10000"}}']);
  expect(jira.statuses["INP-104"]).toBe("Done");
  expect(jira.jira.transitions).toBe(1);
});

test("a failed move is kept and retried", async ({ page, request }) => {
  await configure(request, "atlassian", { transitionMode: "reject" });
  const row = rowOf(page, "INP-104 Invoice export");
  await row.getByRole("button", { name: "Merge", exact: true }).click();
  const panel = page.getByRole("group", { name: "Merge #5" });
  await panel.getByRole("button", { name: "Move INP-104" }).click();
  await panel.getByRole("combobox", { name: "Move INP-104" }).click();
  await page.getByRole("option", { name: "Move to Done" }).click();
  await panel.getByLabel("Resolution (required)").selectOption("10000");
  await panel
    .getByRole("checkbox", { name: "Confirm moving INP-104 to Done" })
    .check();
  await panel.getByRole("button", { name: "Merge & move to Done" }).click();

  const steps = page.getByRole("list", { name: "Merge steps" });
  await expect(
    steps.getByText("Jira rejected the move: Workflow validator failed"),
  ).toBeVisible();
  await expect(steps.getByText("Failed", { exact: true })).toBeVisible();
  expect((await fakeState<GitHubState>(request, "github")).merges).toBe(1);
  expect((await fakeState<JiraState>(request, "atlassian")).statuses).toEqual(
    {},
  );

  await configure(request, "atlassian", { transitionMode: "" });
  await steps.getByRole("button", { name: /^Retry/ }).click();
  await expect(steps.getByText("Failed", { exact: true })).toHaveCount(0);
  await expect(steps.getByText("Done", { exact: true })).toHaveCount(2);

  const jira = await fakeState<JiraState>(request, "atlassian");
  expect(jira.moves).toHaveLength(2);
  expect(jira.statuses["INP-104"]).toBe("Done");
  expect((await fakeState<GitHubState>(request, "github")).merges).toBe(1);
});

test("a head that moved is refused and the row is refreshed", async ({
  page,
  request,
}) => {
  const row = rowOf(page, "Bump dependencies");
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Merge", exact: true }).click();
  const panel = page.getByRole("group", { name: "Merge #9" });
  await panel
    .getByRole("checkbox", { name: "Merge without moving any ticket" })
    .check();

  await configure(request, "github", { headMoved: true });
  const before = (await fakeState<GitHubState>(request, "github")).searches;
  await panel.getByRole("button", { name: "Merge without ticket" }).click();

  await expect(panel.getByRole("alert")).toContainText(
    "The pull request changed since the page loaded",
  );
  await expect(
    panel.getByRole("button", { name: "Merge without ticket" }),
  ).toBeDisabled();
  expect((await fakeState<GitHubState>(request, "github")).merges).toBe(0);
  await expect
    .poll(
      async () => (await fakeState<GitHubState>(request, "github")).searches,
    )
    .toBeGreaterThan(before);
  await expect(
    page.getByRole("button", { name: "Sync", exact: true }),
  ).toBeVisible();

  await panel.getByRole("button", { name: "Cancel" }).click();
  await row.getByRole("button", { name: "Merge", exact: true }).click();
  await panel
    .getByRole("checkbox", { name: "Merge without moving any ticket" })
    .check();
  await panel.getByRole("button", { name: "Merge without ticket" }).click();
  await expect(
    page.getByRole("region", { name: "Recent merges" }),
  ).toContainText("Bump dependencies");
  const github = await fakeState<GitHubState>(request, "github");
  expect(github.merges).toBe(1);
  expect(github.lastMerge.sha).toBe(headOf(9, true));
});

test("a token without Contents write says which permission is missing", async ({
  page,
  request,
}) => {
  const useToken = async (token: string) => {
    await page.goto(APP + "/setup/");
    await page.getByLabel("Personal access token").fill(token);
    await page.getByRole("button", { name: "Validate and save" }).click();
    await expect(page.getByText("Connected as octocat")).toBeVisible();
  };
  await useToken("github_pat_contents_read");
  try {
    await page.goto(APP + "/");
    const row = rowOf(page, "Bump dependencies");
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Merge", exact: true }).click();
    const panel = page.getByRole("group", { name: "Merge #9" });
    await panel
      .getByRole("checkbox", { name: "Merge without moving any ticket" })
      .check();
    await panel.getByRole("button", { name: "Merge without ticket" }).click();

    await expect(panel.getByRole("alert")).toContainText(
      "The token needs the GitHub permission contents: write",
    );
    expect((await fakeState<GitHubState>(request, "github")).merges).toBe(0);
    await expect(row).toBeVisible();
  } finally {
    await useToken("ghp_classic_ok");
  }
});
