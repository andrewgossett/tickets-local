TICKETS LOCAL 0.5.11
===================

Tickets Local is a standalone or LAN-shared dispatch console. It does not require a
database, web server, account, or installer.

It is an AI-assisted hobby project, not an official or certified emergency
system, and is provided without warranty of any kind. See NOTICE.md and
THIRD-PARTY-NOTICES.md inside the app resources for details.

INSTALL ON MAC
--------------

1. Open the signed and notarized Tickets Local DMG.
2. Drag "Tickets Local.app" to Applications.
3. Open Tickets Local normally.

The app is signed with an Apple Developer ID and notarized by Apple. No
Terminal command, quarantine removal, or first-launch preparation is required.

APRS
----

APRS tracking is configured under Settings and on each responder. Choose
Internet only, Local RF only, or Hybrid. Internet mode validates the APRS-IS
login and never transmits. Local RF can use the decoder packaged in this app or
an existing KISS TCP TNC. Hybrid merges both feeds and suppresses duplicates.
An optional RF-to-APRS-IS iGate is off by default. When enabled, it waits for a
verified APRS-IS login and gates eligible radio-heard packets; it never enables
Internet-to-radio transmission, beaconing, or digipeating.
The Situation page shows both connection states, last packets, packet/position
counters, responder updates, and nearby or locally heard stations.

MAPS
----

Drag anywhere on the Situation map—including over a marker—to move it. Use the
mouse wheel or trackpad, double-click, keyboard, or map buttons to zoom. Select
Expand map for an in-window expanded view, or Open map window to detach it and
then use Full screen. The home button returns to the configured default view.
Saved locations, address-based map centering, and KML overlays are available.
Online address search and map tiles require internet access; saved records and
imported KML geometry remain local and work after restart.

WORKFLOW
--------

Active incidents have one-click Next Stage buttons and a five-stage progression
control in the editor. Incidents without a recent saved update are highlighted;
the interval defaults to 10 minutes and can be changed under Settings. The
Situation resource board can optionally be tabbed by responder type.

When an incident or facility is saved without coordinates, Tickets Local makes
one address-lookup attempt and centers the map on a successful match. An active
incident that still cannot be located remains visible in the map's "needs map
locations" panel so it is not silently omitted.

DATA
----

Mac: ~/Library/Application Support/Tickets Local

Use Settings > Download backup to save the append-only operational event log.
Use Settings > Shared operations to keep this installation standalone, make it
the authoritative LAN Host, or connect it as a Client. Host traffic requires
the generated LAN access key. Use Host mode only on a trusted private network.
When finished, use Quit Tickets Local in the sidebar. This safely stops APRS
and the local server and exits the application; closing the browser by itself
does not stop the app.

Project information:
https://github.com/openises/tickets

This independent local implementation is distributed under GNU GPL v2. The
corresponding source is provided with the matching Tickets Local release.
