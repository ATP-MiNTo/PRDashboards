import { expect, test } from "@playwright/test";
import {
  APP,
  configure,
  connectAccounts,
  resetFakes,
  signIn,
  syncUntil,
} from "./helpers";

// TECH_SPEC section 12, flow 4: GitHub answers with a used up rate limit. The dashboard keeps the
// last good rows, says when the limit resets, and recovers on the next sync.

test.beforeAll(async ({ browser, request }) => {
  await connectAccounts(browser, request, "limits");
});

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page, "limits");
  await page.goto(APP + "/");
  await syncUntil(page, "Bump dependencies");
});

for (const [mode, title] of [
  ["reset", "403 with x-ratelimit-reset"],
  ["retry_after", "429 with Retry-After"],
] as const) {
  test(`a ${title} shows the waiting state and recovers`, async ({
    page,
    request,
  }) => {
    await configure(request, "github", { rateLimit: mode });
    await page.getByRole("button", { name: "Refresh", exact: true }).click();

    await expect(page.getByText(/GitHub rate limit reached/)).toBeVisible();
    // The source status is in the popover of the Refresh button, which opens on hover and focus.
    await page.getByRole("button", { name: "Refresh", exact: true }).hover();
    const sources = page.getByRole("status", { name: "Source status" });
    await expect(sources).toContainText(/GitHub rate limited/);
    if (mode === "reset") {
      await expect(sources).toContainText(/resets in \d+m/);
    }
    const row = page
      .locator("tr[data-pr]")
      .filter({ hasText: "Bump dependencies" });
    await expect(row).toBeVisible();
    await expect(row.getByText(/Last data/)).toBeVisible();

    await configure(request, "github", { rateLimit: "" });
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.getByText(/GitHub rate limit reached/)).toHaveCount(0);
    await expect(sources).toContainText("GitHub ok");
    await expect(row.getByText(/Last data/)).toHaveCount(0);
  });
}
