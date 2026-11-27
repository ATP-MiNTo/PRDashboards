# Toasts

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [header.md](header.md), [command-table.md](command-table.md), [theme.md](theme.md).

## Purpose

A short message at the top right of the page that leaves by itself after 3.2 seconds. The header uses it to say what a Refresh found and to confirm the theme choice ([header.md](header.md)). The PRD and spec do not describe it; it is part of the v2 UI migration.

## Data model

No server data and no browser storage. The list of toasts is component state of `ToastProvider`.

## Code map

- [web/src/components/Toast/Toast.tsx](../../web/src/components/Toast/Toast.tsx): `ToastProvider`, `useToast()` (returns `{ show(message, kind?) }`), `TOAST_MS` (3200) and the `ToastKind` type (`info`, `success`, `error`).
- [Toast.module.css](../../web/src/components/Toast/Toast.module.css): the stack and the three solid colours (daisyUI `--in`, `--su`, `--er`), top .75rem, right 1rem, z-index 80, a short pop-in.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): mounts the provider around the dashboard.

## Key flows

1. A caller takes `show` from `useToast()` and calls `show('Refreshed. 2 pull requests changed.', 'success')`. The kind defaults to `info`.
2. The provider adds the toast with a new id and starts a `setTimeout` of `TOAST_MS`. When it fires the toast is removed. Several toasts stack, each leaving after its own time.
3. When the provider unmounts, every pending timer is cleared.

## Outside calls

None.

## Rules and invariants

- Every toast is an element with `role="status"` (polite, so it is read without taking focus). The stack itself is not a live region, so a toast is announced once.
- Outside a provider `useToast()` returns a no-op, so a component that shows toasts also renders on its own (in a test, or on a page without the provider).
- The pop-in animation is off under `prefers-reduced-motion: reduce`.
- Classes only: no `style` attribute and no `<style>` block.
- A message is plain text, never markup.

## Tests

- [Toast.test.tsx](../../web/src/components/Toast/Toast.test.tsx): the status role and the removal after 3.2 seconds, stacking with separate timers, the kind classes and the `info` default, the no-op outside a provider, timers cleared on unmount.
- [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): the refresh and theme toasts.
