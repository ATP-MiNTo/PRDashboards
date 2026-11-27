import { expect, test, type Page } from "@playwright/test";
import {
  APP,
  configure,
  connectAccounts,
  fakeState,
  openSettings,
  resetFakes,
  signIn,
  syncUntil,
} from "./helpers";

// A ticket linked to three pull requests moves only when the third one meets the rule (mode All),
// through the real poller and the rule created in the status sync rules dialog.

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
  page.getByRole("button", { name: "Refresh", exact: true }).click();

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
  const drawer = await openSettings(page);
  await drawer.getByRole("button", { name: "Status sync rules" }).click();
  const dialog = page.getByRole("dialog", { name: "Status sync rules" });
  await dialog.getByRole("button", { name: "Add rule", exact: true }).click();
  await page.getByRole("menuitem", { name: "Blank rule", exact: true }).click();
  const card = dialog.getByRole("listitem", { name: /^Rule 1,/ });
  await card
    .getByRole("combobox", { name: "When", exact: true })
    .selectOption("review_approved");
  await card.getByLabel("Then move it to").selectOption("Ready to Merge");
  await card.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(card.getByRole("region", { name: "Preview" })).toBeVisible();
  // The approved fixture pull requests already match, and they must stay where they are.
  await card
    .getByRole("radio", { name: "Don't move existing tickets" })
    .check();
  await card.getByRole("button", { name: "Save rule" }).click();
  await expect(card.getByText("Unsaved")).toBeHidden();
  await expect(card.getByLabel("Tickets shared by several PRs")).toHaveValue(
    "all",
  );
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).toBeHidden();
  await drawer.getByRole("button", { name: "Collapse sidebar" }).click();

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
  await expect(page.getByText(/Waiting on \d+ of \d+ PRs/)).toHaveCount(0);

  await (await openSettings(page))
    .getByRole("button", { name: "Status sync rules" })
    .click();
  const log = page
    .getByRole("dialog", { name: "Status sync rules" })
    .getByRole("list", { name: "Activity" });
  await expect(log.getByText(/INP-105/).first()).toBeVisible();
  await expect(log.getByText(/by a status sync rule/).first()).toBeVisible();
});
