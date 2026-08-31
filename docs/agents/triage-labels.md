# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

This repo tracks issues as local markdown files (see `issue-tracker.md`), so a "label" is the value of the `Status:` line near the top of an issue file — not a GitHub label.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |
| —                          | `done`               | Every acceptance criterion met, shipped  |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

Edit the right-hand column to match whatever vocabulary you actually use.

## `done` is ours, not a triage role

The five roles above answer "is this ready to be worked on"; no skill sets `done`, because none of them
close a ticket. It is set by hand once every checkbox is ticked and the change is committed, and it is
terminal — a ticket that needs more work goes back to one of the five, it does not stay `done`.

Do not confuse it with the `/wayfinder` vocabulary in `issue-tracker.md` (`claimed` / `resolved`).
Those live on wayfinding tickets, which carry a `Type:` line; implementation tickets never do.
