# Dialog, popup menu and tooltip

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [command-table.md](command-table.md), [merge-and-transition.md](merge-and-transition.md), [review-actions.md](review-actions.md), [ticket-linking.md](ticket-linking.md), [theme.md](theme.md).

## Purpose

Three small shared components that the cards' controls are built from, taken from the approved design:
a centred dialog (the merge dialog and the request-changes dialog), a popup menu opened by a button
(the Action menu, the "My tickets" menu, the method and the ticket status dropdowns of the merge
dialog) and a tooltip (the blocked Merge, the Ready button, the review badge). The PRD and spec do
not describe them; they are part of the v2 UI migration. The older [Dropdown](../../web/src/components/Dropdown/Dropdown.tsx) (a combobox) stays for the settings.

## Data model

None. No server data and no browser storage; all state is component state.

## Code map

- [web/src/components/Modal/Modal.tsx](../../web/src/components/Modal/Modal.tsx) and [Modal.module.css](../../web/src/components/Modal/Modal.module.css): `Modal` (`labelledBy`, `onClose`, `dismissible`), the classes `title`, `subtitle`, `actions`, `hint` and `cancel` that the dialogs' contents use. [Dialogs.module.css](../../web/src/components/CommandTable/Dialogs.module.css) has what goes inside.
- [web/src/components/PopMenu/PopMenu.tsx](../../web/src/components/PopMenu/PopMenu.tsx) and [PopMenu.module.css](../../web/src/components/PopMenu/PopMenu.module.css): `PopMenu`, `MenuTitle`, `MenuNote`, `MenuItem`.
- [web/src/components/Tip/Tip.tsx](../../web/src/components/Tip/Tip.tsx) and [Tip.module.css](../../web/src/components/Tip/Tip.module.css): `Tip`.
- [web/src/components/Dropdown/usePopover.ts](../../web/src/components/Dropdown/usePopover.ts): places a fixed popover next to its anchor (`side` below, above or auto, `align` start or end, `gap`, `matchWidth`), keeps it inside the viewport sideways, follows scroll and resize, and closes it only when the anchor leaves the viewport. The Dropdown, the CI popover and `PopMenu` use it.
- [web/src/lib/focus.ts](../../web/src/lib/focus.ts): `FOCUSABLE`, the selector the focus trap uses. [web/src/components/Modal/ModalHead.tsx](../../web/src/components/Modal/ModalHead.tsx): the top of the design's dialogs, the blue Close (or Done) button pinned to the corner while the content scrolls, then the title with its optional "i" note; the rules, repository and Accounts dialogs use it.

## Key flows

1. `Modal` renders into `document.body` through a portal (a glass card has `backdrop-filter`, which makes it the containing block of fixed children, so a dialog inside a card would be placed and clipped by the card). It moves the focus to the box, keeps Tab and Shift+Tab inside it, closes on Escape and on a click on the scrim (both only while `dismissible`, which the dialogs turn off while their request is out), and gives the focus back to what had it before.
2. `PopMenu` renders a trigger button (`aria-haspopup="menu"`, `aria-expanded`, `aria-controls` while open) and, while open, a `role="menu"` in a portal, placed by `usePopover`. A click, Enter, Space or the arrow keys open it; with `hover` it also opens after 120 ms of hover and closes 250 ms after the pointer leaves (a menu opened by a click stays until it is closed). The focus goes to the first enabled item when it was opened on purpose; the arrow keys move between enabled items; Escape closes it and returns the focus to the button; Tab or a mouse press elsewhere closes it. `onOpen` runs each time it opens, which is where the "My tickets" menu starts loading.
3. `MenuItem` is a `role="menuitem"` button. A disabled one has `aria-disabled` and ignores clicks, but is not skipped by assistive technology. `active` marks the current choice with the neutral fill of the design.
4. `Modal` takes `wide` for the rules and Accounts dialogs (at most 48rem instead of 32rem). `InfoTip` is the design's "i" ghost button: it shows a dark note under it (portalled, placed by `usePopover` with `shift`, the horizontal offset of the placement) on hover and focus, closes on Escape without closing what is around it, and the note is also its accessible description.
5. `Tip` wraps one control and sets `data-tip`; the bubble is drawn by CSS (`::before`) on hover and while a control inside has keyboard focus (`:has(:focus-visible)`), on top (default) or on the left. The generated text has an empty accessible alternative (`content: attr(data-tip) / ''`), so it is not read twice.

## Outside calls

None.

## Rules and invariants

- A tooltip is only a visual hint: a control that has one also carries the text as its accessible description (an `aria-describedby` to a visually hidden element), so nothing depends on hovering. A blocked Merge is `aria-disabled`, not `disabled`, so it can be focused and its reason read.
- A dialog is `role="dialog"` with `aria-modal="true"` and `aria-labelledby` its heading. It does not use the native `<dialog>` element.
- Popovers, menus and dialogs are portalled to the end of the body and are positioned by script on the element (`usePopover`); markup never carries a `style` attribute and there is no `<style>` block. Classes only.
- Animations (the dialog's scale-in, the menu's fade) are off under `prefers-reduced-motion: reduce`.
- Text is plain: no `dangerouslySetInnerHTML`.
- A dialog opened from a card closes when its card unmounts; a merge launch keeps the card mounted ([merge-and-transition.md](merge-and-transition.md)).

## Tests

- [Modal.test.tsx](../../web/src/components/Modal/Modal.test.tsx): role and focus, Escape and the scrim, `dismissible`, the Tab trap, focus return, `wide`.
- [InfoTip.test.tsx](../../web/src/components/InfoTip/InfoTip.test.tsx): the description, hover and focus, Escape.
- [PopMenu.test.tsx](../../web/src/components/PopMenu/PopMenu.test.tsx): open, select, disabled items, arrow keys, Escape, outside press, Tab, toggle, disabled trigger, hover open and close, a click that pins it.
- [Tip.test.tsx](../../web/src/components/Tip/Tip.test.tsx), [usePopover.test.tsx](../../web/src/components/Dropdown/usePopover.test.tsx) (the placements, the shift and the horizontal clamp), [Dropdown.test.tsx](../../web/src/components/Dropdown/Dropdown.test.tsx).
- The components in use are covered by the tests listed in [command-table.md](command-table.md).
