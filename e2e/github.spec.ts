import { expect, test } from "@playwright/test";
import {
  APP,
  configure,
  disconnectAll,
  fakeState,
  resetFakes,
  signIn,
} from "./helpers";

// TECH_SPEC section 12, flow 1: connecting GitHub. The authorize endpoint is spelled "localhost"
// and the app "127.0.0.1", so the redirect back to the callback is a cross-site navigation and the
// SameSite=Strict session cookie is not sent with it.

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page);
  await disconnectAll(page);
});

test("OAuth sign-in through the cross-site callback and the finish page", async ({
  page,
  request,
}) => {
  await page.getByRole("link", { name: "Connect GitHub" }).click();
  await expect(page).toHaveURL(
    /\/auth\/done\/\?(?=.*provider=github)(?=.*result=ok)/,
  );
  await expect(page.getByRole("status")).toHaveText("GitHub is connected.");

  // The code and state are gone from the address bar and from history.
  expect(page.url()).not.toContain("code=");
  expect(page.url()).not.toContain("state=");

  await page.getByRole("link", { name: "Back to the dashboard" }).click();
  await expect(page.getByText("Connected as octocat (OAuth)")).toBeVisible();

  const state = await fakeState<{
    exchanges: number;
    lastCodeVerifier: string;
  }>(request, "github");
  expect(state.exchanges).toBe(1);
  expect(state.lastCodeVerifier.length).toBeGreaterThanOrEqual(43);
});

test("a tampered state is rejected and nothing is connected", async ({
  page,
  request,
}) => {
  await configure(request, "github", { tamperState: true });
  await page.getByRole("link", { name: "Connect GitHub" }).click();
  await expect(page.getByRole("status")).toHaveText(
    "The sign-in link expired or was already used. Start again.",
  );
  expect(
    (await fakeState<{ exchanges: number }>(request, "github")).exchanges,
  ).toBe(0);
  await page.goto(APP + "/");
  await expect(page.getByText("Not connected").first()).toBeVisible();
});

test("a used state cannot be replayed", async ({ page, request }) => {
  await page.getByRole("link", { name: "Connect GitHub" }).click();
  await expect(page.getByRole("status")).toHaveText("GitHub is connected.");
  const { lastCallbackUrl } = await fakeState<{ lastCallbackUrl: string }>(
    request,
    "github",
  );
  expect(lastCallbackUrl).toContain("/oauth/github/callback?");

  await page.goto(lastCallbackUrl);
  await expect(page.getByRole("status")).toHaveText(
    "The sign-in link expired or was already used. Start again.",
  );
  expect(
    (await fakeState<{ exchanges: number }>(request, "github")).exchanges,
  ).toBe(1);
});

test("denying the authorization ends on a clear message", async ({
  page,
  request,
}) => {
  await configure(request, "github", { deny: true });
  await page.getByRole("link", { name: "Connect GitHub" }).click();
  await expect(page.getByRole("status")).toHaveText(
    "GitHub access was not granted.",
  );
  expect(
    (await fakeState<{ exchanges: number }>(request, "github")).exchanges,
  ).toBe(0);
});

test.describe("access token", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(APP + "/setup/");
  });

  const submit = async (
    page: import("@playwright/test").Page,
    token: string,
  ) => {
    await page.getByLabel("Personal access token").fill(token);
    await page.getByRole("button", { name: "Validate and save" }).click();
  };

  test("a classic token with the needed scopes connects", async ({ page }) => {
    await submit(page, "ghp_classic_ok");
    await expect(page.getByText("Connected as octocat")).toBeVisible();
    await page.goto(APP + "/");
    await expect(
      page.getByText("Connected as octocat (access token)"),
    ).toBeVisible();
  });

  test("a classic token without read:org is rejected", async ({ page }) => {
    await submit(page, "ghp_classic_no_org");
    await expect(page.locator('p[role="alert"]')).toHaveText(
      "The token lacks scopes the app needs (repo and read:org).",
    );
  });

  test("a fine-grained token connects with a warning", async ({ page }) => {
    await submit(page, "github_pat_finegrained");
    await expect(page.getByText("Connected as octocat")).toBeVisible();
    await expect(
      page
        .getByText(
          "Fine-grained tokens only see the repositories they were granted.",
        )
        .last(),
    ).toBeVisible();
  });

  test("an unknown token is rejected", async ({ page }) => {
    await submit(page, "ghp_nope");
    await expect(page.locator('p[role="alert"]')).toHaveText(
      "GitHub rejected the token.",
    );
  });
});
