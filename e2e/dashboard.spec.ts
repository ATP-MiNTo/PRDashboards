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

// The roles come from which of the two searches found each pull request: #101 is only assigned,
// #7 only opened by the user and #102 both.
test("the Show switch follows the roles from the two searches", async ({
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

  await page.getByRole("button", { name: /^Show, now Everything/ }).click();
  await expect(
    page.getByRole("button", { name: /^Show, now Assigned to me/ }),
  ).toBeVisible();
  await expect(rows("INP-101 Add login")).toBeVisible();
  await expect(rows("INP-102 Single sign-on")).toBeVisible();
  await expect(rows("WIP schema cleanup")).toHaveCount(0);

  await page.getByRole("button", { name: /^Show, now Assigned to me/ }).click();
  await expect(
    page.getByRole("button", { name: /^Show, now Opened by me/ }),
  ).toBeVisible();
  await expect(rows("WIP schema cleanup")).toBeVisible();
  await expect(rows("INP-102 Single sign-on")).toBeVisible();
  await expect(rows("INP-101 Add login")).toHaveCount(0);
});
