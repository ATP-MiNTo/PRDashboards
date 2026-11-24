import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  expect,
  type APIRequestContext,
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

// Session creation is rate limited (10 a minute), so the suite signs in once through the launch
// URL, where the token travels in the fragment, and later tests reuse that session's cookies.
let cookies: Awaited<ReturnType<BrowserContext["cookies"]>> | undefined;

export async function signIn(page: Page) {
  if (cookies) {
    await page.context().addCookies(cookies);
    await page.goto(`${APP}/`);
  } else {
    const { token } = JSON.parse(
      readFileSync(join(dataDir, "launch.token"), "utf8"),
    ) as {
      token: string;
    };
    await page.goto(`${APP}/auth/launch/#t=${token}`);
  }
  await expect(page.getByText("Signed in", { exact: true })).toBeVisible();
  cookies ??= await page.context().cookies();
}

// Disconnects whatever a previous test left behind, through the UI's own API.
export async function disconnectAll(page: Page) {
  for (const name of ["Disconnect GitHub", "Disconnect Jira"]) {
    const button = page.getByRole("button", { name });
    if (await button.isVisible()) {
      await button.click();
      await expect(button).toBeHidden();
    }
  }
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
