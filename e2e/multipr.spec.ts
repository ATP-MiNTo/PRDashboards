import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  configure,
  connectAccounts,
  fakeState,
  resetFakes,
  signIn,
  syncUntil,
} from "./helpers";

// A ticket linked to three pull requests moves only when the third one meets the rule (mode All),
// through the real poller and the rule created in the settings drawer.

type JiraState = {
  statuses: Record<string, string>;
  moves: string[];
  jira: { moves: number; transitions: number };
};

test.beforeAll(async ({ browser, request }) => {
  await connectAccounts(browser, request, "multipr");
});

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await configure(request, "github", { multiPr: true, approved: [] });
  await signIn(page, "multipr");
  await page.goto(APP + "/");
  await syncUntil(page, "INP-105 proxy");
});

const sync = (page: Page) =>
  page.getByRole("button", { name: "Sync", exact: true }).click();

const approve = async (
  page: Page,
  request: Parameters<typeof configure>[0],
  numbers: number[],
) => {
  await configure(request, "github", { multiPr: true, approved: numbers });
  await sync(page);
};

test("moves INP-105 only after the third approval", async ({
  page,
  request,
}) => {
  const before = (await fakeState<JiraState>(request, "atlassian")).jira;
  await page.getByRole("button", { name: "Settings" }).click();
  const drawer = page.getByRole("dialog", { name: "Settings" });
  await drawer.getByRole("button", { name: "Add rule", exact: true }).click();
  const form = drawer.getByRole("form", { name: "New rule" });
  await form.getByLabel("When").selectOption("review_approved");
  await form.getByLabel("Move the ticket to").fill("Ready to Merge");
  await form.getByRole("button", { name: "Preview" }).click();
  await expect(form.getByRole("region", { name: "Preview" })).toBeVisible();
  // The approved fixture pull requests already match, and they must stay where they are.
  await form
    .getByRole("radio", { name: "Don't move existing tickets" })
    .check();
  await form.getByRole("button", { name: "Save rule" }).click();
  await expect(form).toBeHidden();
  await expect(
    drawer.getByRole("button", { name: /: All$/ }),
  ).toHaveAttribute("aria-pressed", "true");
  await drawer.getByRole("button", { name: "Close settings" }).click();

  await approve(page, request, [34]);
  await expect(
    page.getByText("Waiting on 2 of 3 PRs: be #12, proxy #56").first(),
  ).toBeVisible();
  await approve(page, request, [34, 12]);
  await expect(
    page.getByText("Waiting on 1 of 3 PRs: proxy #56").first(),
  ).toBeVisible();
  let jira = await fakeState<JiraState>(request, "atlassian");
  expect(jira.moves).toEqual([]);
  expect(jira.statuses["INP-105"]).toBeUndefined();
  expect(jira.jira.transitions - before.transitions).toBe(0);

  await approve(page, request, [34, 12, 56]);
  await expect
    .poll(async () => (await fakeState<JiraState>(request, "atlassian")).moves)
    .toHaveLength(1);
  jira = await fakeState<JiraState>(request, "atlassian");
  expect(jira.moves[0]).toMatch(/^INP-105:41:/);
  expect(jira.statuses["INP-105"]).toBe("Ready to Merge");
  expect(jira.jira.transitions - before.transitions).toBe(1);
  expect(jira.jira.moves - before.moves).toBe(1);
  await expect(page.getByText(/Waiting on/)).toHaveCount(0);

  await page.getByRole("button", { name: "Settings" }).click();
  const log = page
    .getByRole("dialog", { name: "Settings" })
    .getByRole("list", { name: "Activity" });
  await expect(log.getByText(/INP-105/).first()).toBeVisible();
  await expect(log.getByText(/by a status sync rule/).first()).toBeVisible();
});
