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

**Banner**:
The map's own box, drawn above its tickets, that stands for the map issue and can be selected like a ticket.
_Avoid_: Title bar, map card, map node

**Strip**:
The rows under the map: the legend and key hints, then the selected ticket's title and facts.

**Selection**:
The highlighted ticket or banner on the map, or the cursor on a related issue in the detail view.
_Avoid_: Focus

**Detail view**:
The full-pane view of one issue: its header, relationships, sub-issues and body.
_Avoid_: Panel, popup, modal

**Focus**:
The issue the detail view is showing.

**Mini-map**:
The map filtered to the focus ticket and its direct neighbours, drawn like the map.
_Avoid_: Neighbourhood, subgraph
