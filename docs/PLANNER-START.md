# Plan start dates and the FamilieHub icon

The planner asks when the plan should start. The default is **today in
Europe/Copenhagen**, with shortcuts for today, tomorrow and next Monday.
Any valid weekday is accepted. The preview and saved result show the chosen date.
No existing family data is changed by installing this update.

## Scheduling

The weekly weekday pattern is retained. Occurrences in the elapsed part of the
first week begin on their next matching weekday, not retroactively. Whole-week
chores become visible only on the chosen start date. Their first window ends
on Sunday. The first, possibly shorter week's effort is shown separately.
Rotation follows calendar weeks, starting on Monday after the selected number
of weeks; its first date is shown explicitly. No rotation means no owner change.
Choosing a past date intentionally is supported but displays a warning.

Both task listing and completion reject planner occurrences before their start.
This also prevents reminder generation for the days preceding the start date.
Manual and legacy whole-week tasks without a planner start retain their behavior.
A changed date invalidates the previous preview; an outdated pending response
cannot restore it. Saving uses the exact request represented by that preview.
The existing MCP move operation still moves complete calendar weeks and keeps
an original midweek start on the same weekday.

## Icon

`web/icon.svg` remains the single original house design. The ICO fallback and
180-pixel Apple touch icon are derived from it. The main HTML explicitly lists
SVG, ICO and Apple touch versions, with versioned asset URLs for browser caches.
The conventional `/favicon.ico` path also works.

## Verification

Go integration tests cover all seven start weekdays, pre-start visibility and
completion, first-week totals, persisted starts, Monday rotation, no-rotation
plans, legacy whole-week behavior, reminder exclusion and static icon responses.
Node tests cover Copenhagen's date boundary, shortcuts, DST and year changes.
MCP regression coverage verifies moving a plan that starts midweek.
