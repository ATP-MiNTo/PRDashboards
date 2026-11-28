import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  expect,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from "@playwright/test";

export const APP = "http://127.0.0.1:4317";
const GITHUB_ADMIN = "http://127.0.0.1:4411/__admin";
const ATLASSIAN_ADMIN = "http://127.0.0.1:4412/__admin";
const dataDir = join(__dirname, "..", ".e2e-data");

export type Fake = "github" | "atlassian";
const admin = (fake: Fake) =>
  fake === "github" ? GITHUB_ADMIN : ATLASSIAN_ADMIN;

export async function resetFakes(request: APIRequestContext) {
  for (const fake of ["github", "atlassian"] as const) {
    const res = await request.post(`${admin(fake)}/reset`);
    expect(res.status()).toBe(204);
  }
}

export async function configure(
  request: APIRequestContext,
  fake: Fake,
  config: object,
) {
  const res = await request.post(`${admin(fake)}/config`, { data: config });
  expect(res.status()).toBe(204);
}

export async function fakeState<T = Record<string, unknown>>(
  request: APIRequestContext,
  fake: Fake,
): Promise<T> {
  const res = await request.get(`${admin(fake)}/state`);
  expect(res.ok()).toBe(true);
  return (await res.json()) as T;
}

// Session creation is rate limited (10 a minute), so each named session signs in once through the
// launch URL, where the token travels in the fragment, and later tests reuse its cookies. Writes
// are limited per session (30 a minute), so a spec that makes many writes asks for its own name.
const sessions = new Map<
  string,
  Awaited<ReturnType<BrowserContext["cookies"]>>
>();

export async function signIn(page: Page, name = "shared") {
  const saved = sessions.get(name);
  if (saved) {
    await page.context().addCookies(saved);
  } else {
    const { token } = JSON.parse(
      readFileSync(join(dataDir, "launch.token"), "utf8"),
    ) as {
      token: string;
    };
    await page.goto(`${APP}/auth/launch/#t=${token}`);
    await expect(page).not.toHaveURL(/\/auth\/launch/);
  }
  await page.goto(`${APP}/accounts/`);
  await expect(page.getByText("Signed in", { exact: true })).toBeVisible();
  if (!saved) sessions.set(name, await page.context().cookies());
}

// Disconnects whatever a previous test left behind, through the UI's own API.
export async function disconnectAll(page: Page) {
  for (const [name, connect] of [
    ["Disconnect GitHub", "Connect GitHub"],
    ["Disconnect Jira", "Connect Jira"],
  ] as const) {
    const button = page.getByRole("button", { name });
    // The panel loads after "Signed in" shows, so wait until it says which state it is in.
    await expect(
      button.or(page.getByRole("link", { name: connect })),
    ).toBeVisible();
    if (await button.isVisible()) {
      await button.click();
      await expect(button).toBeHidden();
    }
  }
}

// Makes the token the only GitHub token: with several tokens a new one is added next to the others
// and the repositories go to the token with the highest role (the first on a tie), so a test of a
// restricted token disconnects GitHub first, keeping Jira.
export async function useOnlyGitHubToken(page: Page, token: string) {
  await page.goto(APP + "/accounts/");
  const button = page.getByRole("button", { name: "Disconnect GitHub" });
  await expect(
    button.or(page.getByRole("link", { name: "Connect GitHub" })),
  ).toBeVisible();
  if (await button.isVisible()) {
    await button.click();
    await expect(button).toBeHidden();
  }
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill(token);
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();
}

export async function saveClient(
  page: Page,
  title: string,
  id: string,
  secret: string,
) {
  const section = page.getByRole("region", { name: title });
  await section.getByLabel("Client ID").fill(id);
  await section.getByLabel("Client secret").fill(secret);
  await section.getByRole("button", { name: "Save credentials" }).click();
  await expect(section.getByText("Saved")).toBeVisible();
}

// Connects a token to GitHub and OAuth to Jira once for a named session, so the tests of a spec
// only reset the fakes and sync.
export async function connectAccounts(
  browser: Browser,
  request: APIRequestContext,
  session: string,
) {
  const page = await (await browser.newContext()).newPage();
  await resetFakes(request);
  await signIn(page, session);
  await disconnectAll(page);
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();
  await page.goto(APP + "/accounts/");
  await page.getByRole("link", { name: "Connect Jira" }).click();
  await expect(page.getByRole("status")).toHaveText("Jira is connected.");
  await page.context().close();
}

// Presses Refresh until a row with the text shows, because the first refresh can still be running.
export async function syncUntil(page: Page, text: string) {
  await expect(async () => {
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(
      page.locator("article[data-pr]").filter({ hasText: text }).first(),
    ).toBeVisible({ timeout: 3000 });
  }).toPass({ intervals: [1000, 2000, 3000], timeout: 15_000 });
}

/** Opens the Settings sidebar with the burger (it is already open on a wide screen until folded) and returns it. */
export async function openSettings(page: Page) {
  const burger = page.getByRole("button", { name: "Settings sidebar" });
  if ((await burger.getAttribute("aria-expanded")) !== "true") {
    await burger.click();
  }
  const sidebar = page.getByRole("complementary", { name: "Settings" });
  await sidebar.waitFor();
  return sidebar;
}
