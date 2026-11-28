import { expect, test } from "@playwright/test";
import { APP, configure, disconnectAll, resetFakes, signIn } from "./helpers";

// TECH_SPEC section 12, flow 3: an organization that enforces SSO. The fake GitHub leaves out the
// pull requests of acme-corp and answers with the SAML header, as GitHub does. The banner links to
// the organization's SSO page and the other rows load.

test.beforeEach(async ({ page, request }) => {
  await resetFakes(request);
  await signIn(page);
  await disconnectAll(page);
});

test("an SSO-blocked organization shows the banner while the other rows load", async ({
  page,
  request,
}) => {
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();

  await page.goto(APP + "/");
  await expect(page.getByText("INP-104 Invoice export")).toBeVisible();
  await expect(page.getByText("has not authorized")).toHaveCount(0);
  // Opened by the user and assigned to nobody: only the author search finds it.
  await expect(page.getByText("WIP schema cleanup")).toBeVisible();

  await configure(request, "github", { sso: true });
  await page.getByRole("button", { name: "Refresh", exact: true }).click();

  const banner = page.getByRole("alert").filter({ hasText: "acme-corp" });
  await expect(banner).toContainText("acme-corp has not authorized this token");
  await expect(banner.getByRole("link", { name: "Authorize" })).toHaveAttribute(
    "href",
    "https://github.com/orgs/acme-corp/sso",
  );
  await expect(page.getByText("INP-104 Invoice export")).toBeHidden();
  await expect(page.getByText("INP-101 Add login")).toBeVisible();
  await expect(page.getByText("INP-102 Single sign-on")).toBeVisible();
  // The blocked Merge button says why through its accessible description (and a tooltip).
  await expect(
    page
      .locator("article[data-pr]")
      .filter({ hasText: "INP-102 Single sign-on" })
      .getByRole("button", { name: "Merge" }),
  ).toHaveAccessibleDescription("Merge after #101");
});

// The roles come from which of the three searches found each pull request: #101 is only assigned,
// #7 only opened by the user, #102 both, and "Tighten rate limiter" (#10 in acme/api) and "Fix
// focus ring on tabs" (#104 in acme/web) are only review requested.
test("the Show switch follows the roles from the three searches", async ({
  page,
}) => {
  await page.goto(APP + "/setup/");
  await page.getByLabel("Personal access token").fill("ghp_classic_ok");
  await page.getByRole("button", { name: "Validate and save" }).click();
  await expect(page.getByText("Connected as octocat")).toBeVisible();

  await page.goto(APP + "/");
  const rows = (title: string) =>
    page.locator("article[data-pr]").filter({ hasText: title });
  await expect(rows("INP-101 Add login")).toBeVisible();

  // All lists everything the three searches found.
  await expect(
    page.getByRole("button", { name: /^Show, now All/ }),
  ).toBeVisible();
  await expect(rows("INP-101 Add login")).toBeVisible();
  await expect(rows("WIP schema cleanup")).toBeVisible();
  await expect(rows("Tighten rate limiter")).toBeVisible();
  await expect(rows("Fix focus ring on tabs")).toBeVisible();

  // Mine: assigned to the user or opened by the user, never a review-only pull request.
  await page.getByRole("button", { name: /^Show, now All/ }).click();
  await expect(
    page.getByRole("button", { name: /^Show, now Mine/ }),
  ).toBeVisible();
  await expect(rows("INP-101 Add login")).toBeVisible();
  await expect(rows("INP-102 Single sign-on")).toBeVisible();
  await expect(rows("WIP schema cleanup")).toBeVisible();
  await expect(rows("Tighten rate limiter")).toHaveCount(0);
  await expect(rows("Fix focus ring on tabs")).toHaveCount(0);

  // To review: only the pull requests the user is asked to review.
  await page.getByRole("button", { name: /^Show, now Mine/ }).click();
  await expect(
    page.getByRole("button", { name: /^Show, now To review/ }),
  ).toBeVisible();
  await expect(rows("Tighten rate limiter")).toBeVisible();
  await expect(rows("Fix focus ring on tabs")).toBeVisible();
  await expect(rows("INP-101 Add login")).toHaveCount(0);
  await expect(rows("WIP schema cleanup")).toHaveCount(0);
  await expect(page.getByRole("group", { name: "Summary" })).toContainText(
    "Showing2",
  );
});
