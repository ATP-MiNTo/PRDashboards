import { expect, test } from "@playwright/test";
import {
  APP,
  configure,
  disconnectAll,
  fakeState,
  resetFakes,
  signIn,
} from "./helpers";

// TECH_SPEC section 12, flow 2: connecting Jira, choosing a site, refreshing tokens.

type State = {
  exchanges: number;
  refreshes: number;
  rejectedRefreshes: number;
  currentRefreshToken: string;
};

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page);
  await disconnectAll(page);
});

test("sign-in with one site connects", async ({ page, request }) => {
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.getByRole("link", { name: "Back to accounts" }).click();
  await expect(page.getByText("Connected to 1 site")).toBeVisible();
  expect((await fakeState<State>(request, "atlassian")).exchanges).toBe(1);
});

test("two sites require a pick, and the pick is kept", async ({
  page,
  request,
}) => {
  await configure(request, "atlassian", { sites: 2, expiresIn: 3600 });
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.getByRole("link", { name: "Back to accounts" }).click();

  const select = page.getByLabel("Jira site");
  await expect(select).toHaveValue("");
  const options = await select
    .locator('option:not([value=""])')
    .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
  expect(options).toHaveLength(2);
  await select.selectOption(options[1]!);
  await expect(select).toHaveValue(options[1]!);

  await page.reload();
  await expect(page.getByLabel("Jira site")).toHaveValue(options[1]!);
});

test("an expiring token is refreshed and the refresh token rotates", async ({
  page,
  request,
}) => {
  await configure(request, "atlassian", { expiresIn: 1, sites: 1 });
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.getByRole("link", { name: "Back to accounts" }).click();

  // Connecting starts a refresh, whose Jira lookup may already have rotated the token once.
  const before = await fakeState<State>(request, "atlassian");

  await page.getByRole("button", { name: "Check Jira" }).click();
  await expect
    .poll(async () => (await fakeState<State>(request, "atlassian")).refreshes)
    .toBeGreaterThan(before.refreshes);
  const after = await fakeState<State>(request, "atlassian");
  expect(after.currentRefreshToken).not.toBe(before.currentRefreshToken);
  await expect(page.getByText("Connected to 1 site")).toBeVisible();
});

test("a rejected refresh asks to reconnect, and reconnecting recovers", async ({
  page,
  request,
}) => {
  await configure(request, "atlassian", { expiresIn: 1, sites: 1 });
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.getByRole("link", { name: "Back to accounts" }).click();

  await configure(request, "atlassian", {
    expiresIn: 3600,
    sites: 1,
    refreshFail: true,
  });
  await page.getByRole("button", { name: "Check Jira" }).click();
  await expect(
    page.getByText("The Jira authorization expired. Reconnect to continue."),
  ).toBeVisible();
  expect(
    (await fakeState<State>(request, "atlassian")).rejectedRefreshes,
  ).toBeGreaterThan(0);

  await configure(request, "atlassian", { expiresIn: 3600, sites: 1 });
  await page.getByRole("link", { name: "Reconnect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.getByRole("link", { name: "Back to accounts" }).click();
  await expect(page.getByText("Connected to 1 site")).toBeVisible();
});

test("denying the authorization ends on a clear message", async ({
  page,
  request,
}) => {
  await configure(request, "atlassian", {
    deny: true,
    sites: 1,
    expiresIn: 3600,
  });
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText(
    "Jira access was not granted.",
  );
  await page.goto(APP + "/accounts/");
  await expect(page.getByText("Not connected").last()).toBeVisible();
});
