# wayfinder-map

A read-only terminal viewer that draws a Wayfinder map from GitHub Issues as a graph of its tickets and their blockers.

## Wayfinder

**Map**:
An issue labelled `wayfinder:map` that charts the way to one destination; its tickets are its sub-issues.
_Avoid_: Plan, epic, board

**Ticket**:
A sub-issue of a map that asks one question to resolve.
_Avoid_: Task (that's one ticket type), node, card

**Ticket type**:
The kind of work a ticket's `wayfinder:*` label names: research, prototype, grilling or task. A ticket without one has type `?`.

**Blocker**:
A ticket that another ticket is blocked by, through GitHub's native issue dependency. It may live in another repo.
_Avoid_: Parent, prerequisite

## Ticket state

Every ticket is in exactly one state, checked in this order.

**Done**:
A closed ticket, even if it is still assigned.

**Claimed**:
An open ticket with an assignee.
_Avoid_: In progress, taken

**Blocked**:
An open, unassigned ticket with at least one open blocker.

**Frontier**:
An open, unassigned ticket with no open blockers: the tickets that are ready to take.
_Avoid_: Ready (except as a short count label), available

## Viewing

**Picker**:
The list of a repo's open maps that you choose from when no map is named.

**Look**:
How tickets are drawn in the graph: as **cards** (box with title) or **compact** (circle with type icon and number).
_Avoid_: Theme, style, mode
