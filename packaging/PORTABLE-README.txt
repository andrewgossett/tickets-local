TICKETS LOCAL 0.5.15
===================

Tickets Local is a standalone or LAN-shared dispatch console. It does not require a
database, web server, account, or installer.

It is an AI-assisted hobby project, not an official or certified emergency
system, and is provided without warranty of any kind. See NOTICE.md and
THIRD-PARTY-NOTICES.md for details.

WINDOWS
-------
Double-click "Tickets Local.exe". Your normal browser opens automatically.
Keep the application running while you use the console.

MAC
---
Before the first launch, Control-click "Prepare Tickets Local.command", choose
Open, and let it repair the app's local signature. Then open
"Tickets Local.app". If macOS still blocks it, Control-click the app and choose
Open, or use System Settings > Privacy & Security > Open Anyway.

The preparation step uses the built-in macOS codesign tool and installs no
software. Verify the release checksum before approving an unsigned build.

APRS
----
APRS tracking is configured under Settings and on each responder. Choose
Internet only, Local RF only, or Hybrid. Internet mode uses a verified APRS-IS
login. Local RF can connect to an existing Dire Wolf or compatible KISS TCP
TNC. A signed Mac release may also include the built-in Dire Wolf decoder.
The optional RF-to-APRS-IS iGate is off by default and never enables
Internet-to-radio transmission, beaconing, or digipeating.

MAPS
----
Saved locations, address-based map centering, and KML overlays are available in
the console. Online address search and map tiles require internet access; saved
records and imported KML geometry remain local and work after restart.
The Situation map can also be opened in a separate window and placed full
screen.

WORKFLOW
--------
Active incidents have one-click Next Stage buttons and configurable visual
alerts when they have not received a recent update. The default interval is 10
minutes. The Situation resource board can optionally be tabbed by responder
type.

DATA
----
Windows: %AppData%\\Tickets Local
Mac:     ~/Library/Application Support/Tickets Local

Use Settings > Download backup to save the append-only operational event log.
Use Settings > Shared operations to keep this installation standalone, make it
the authoritative LAN Host, or connect it as a Client. Host traffic requires
the generated LAN access key. Use Host mode only on a trusted private network.
When finished, use Quit Tickets Local in the sidebar to stop APRS, shut down
the local server safely, and exit the application process.

Project information:
https://github.com/openises/tickets

This independent local implementation is distributed under GNU GPL v2.
The corresponding source is provided in the matching Tickets Local source
archive distributed with this build.
