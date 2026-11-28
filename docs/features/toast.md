# Toasts

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [header.md](header.md), [command-table.md](command-table.md), [theme.md](theme.md).

## Purpose

A short message at the top right of the page, under the floating header pills (never over them). An information or success message leaves by itself after 3.2 seconds, a time that stops while the pointer is over it or the focus is inside it; an error stays until it is dismissed. The header uses it to say what a Refresh found and to confirm the theme choice ([header.md](header.md)). The cards use it for the outcome of their actions: "You approved #N" and "You requested changes on #N" ([review-actions.md](review-actions.md)), "#N is no longer a draft" and the error of a refused review action or Ready, the summary after a merge ("#490 merged, INP-104 moved to Done", an error toast that names the failed step and says where to retry, or "#490 added to the merge queue", [merge-and-transition.md](merge-and-transition.md)), and a failed ticket link ([ticket-linking.md](ticket-linking.md)). The PRD and spec do not describe it; it is part of the v2 UI migration.

## Data model

No server data and no browser storage. The list of toasts is component state of `ToastProvider`.

## Code map

- [web/src/components/Toast/Toast.tsx](../../web/src/components/Toast/Toast.tsx): `ToastProvider`, `ToastItem` (one toast and its timer), `useToast()` (returns `{ show(message, kind?) }`), `TOAST_MS` (3200) and the `ToastKind` type (`info`, `success`, `error`).
- [Toast.module.css](../../web/src/components/Toast/Toast.module.css): the stack and the three solid colours (daisyUI `--in`, `--su`, `--er`), top `--header-bottom` (each toast has .5rem above it), right `--header-edge` (the right edge of the right header pill), right-aligned, z-index 80 (above the header at 30 and the modals at 70), a short slide in from the right.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): mounts the provider around the dashboard.

## Key flows

1. A caller takes `show` from `useToast()` and calls `show('Refreshed. 2 pull requests changed.', 'success')`. The kind defaults to `info`.
2. The provider adds the toast with a new id. An information or success toast is drawn inside the live region and starts a `setTimeout` of `TOAST_MS`; when it fires the toast is removed. While the pointer is over the toast or the focus is inside it the timer is stopped and the time left is kept; when both are gone it starts again with that time. Several toasts stack, each with its own timer. An error is drawn outside the live region as its own `role="alert"`, has no timer and a visible close button named "Dismiss" (a ×) that removes it.
3. When the provider unmounts, every pending timer is cleared (each toast clears its own).

## Outside calls

None.

## Rules and invariants

- One live region (`role="status"`, `aria-live="polite"`) is mounted with the provider and stays in the page, empty or not, so a screen reader is already listening when a message arrives; information and success toasts are plain elements inside it. An error is `role="alert"` (assertive) in the stack beside it, not inside the region, so it is announced once. There is therefore always exactly one `status` element from the toasts, which is what the e2e specs and tests of other features resolve.
- Outside a provider `useToast()` returns a no-op, so a component that shows toasts also renders on its own (in a test, or on a page without the provider).
- The stack is `position: fixed` and starts below the header: `--header-bottom` (pill top 22px + pill height 44px = 66px) and `--header-edge` (the header's side gutter, 16px or the safe-area inset, plus the pill's 12px) are tokens in [globals.css](../../web/src/app/globals.css), derived from `--pill-top` and `--pill-h`, which the header pills use too. Toasts stack downward and at narrow widths stay right-aligned inside the same gutter (`max-width: 100vw` minus both gutters).
- The pop-in animation is off under `prefers-reduced-motion: reduce`.
- Classes only: no `style` attribute and no `<style>` block.
- A message is plain text, never markup.

## Tests

- [Toast.test.tsx](../../web/src/components/Toast/Toast.test.tsx): the persistent live region and the removal after 3.2 seconds, stacking with separate timers, the kind classes and the `info` default, an error as an alert that stays until Dismiss is pressed, the pause and resume on hover and on focus, the no-op outside a provider, timers cleared on unmount.
- [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): the refresh and theme toasts.
