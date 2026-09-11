# Tickets Local

Tickets Local is a lightweight standalone or LAN-shared CAD application inspired by
[Tickets CAD / OpenISES](https://openises.sourceforge.net/) and its
[GPL-licensed source repository](https://github.com/openises/tickets).

It replaces the PHP + Apache/IIS + MySQL installation with one small executable.
The executable starts its own web service, opens the operator's normal browser,
and stores an append-only operational record in the user's application-data
folder. Standalone and Client browser interfaces remain loopback-only; an
explicit Host role can share one authoritative data store with authenticated
Tickets Local clients on a trusted private LAN.

> **Project status:** v0.5 is a functional local/LAN operations build, not a
> drop-in replacement for every Tickets CAD v3.44 or v4.0 feature. It is
> appropriate for evaluation, exercises, volunteer-event dispatch, and local
> workflow development. Validate it against your organization's policies before
> operational public-safety use.

## Download Tickets Local

No GitHub account is required. Choose the download for your computer:

- **macOS:** [Download the signed and Apple-notarized universal DMG](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.16/Tickets-Local-0.5.16-macOS-universal-notarized.dmg)
- **Windows:** [Download the Windows x64 application](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.15/Tickets-Local-0.5.15-Windows-x64.exe)
- **Windows ZIP:** [Download the portable Windows x64 ZIP](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.15/Tickets-Local-0.5.15-Windows-x64-unsigned.zip)

See the [latest release page](https://github.com/andrewgossett/tickets-local/releases/latest)
for checksums, release notes, and every available package.

## What works

- Incident creation, editing, severity, lifecycle status, and automatic numbering
- Incident-specific ICS command and general staff role assignments
- Configurable Standalone, authoritative Host, and connected Client LAN roles
- Shared-key Host protection, Client connection testing, live-event forwarding,
  polling fallback, and recently active Client status
- Automatic Settings connection tester for the local app, Host and Clients,
  APRS-IS, local RF, NWS weather and radar, water, mesh feeds, and sensors
- Selectable amateur-repeater, GMRS-repeater, and read-only MeshCore-node map layers
- Concurrent-edit conflict protection for core operational records
- One-click incident stage progression and configurable no-update alerts
- Responder records, capabilities, status, incident assignment, guarded deletion,
  and click-or-drag map positioning
- Optional resource-board tabs grouped by responder type
- Internet-only, Local RF, and Hybrid APRS source modes
- Verified APRS-IS tracking for selected responder callsigns
- Independent APRS callsign watchlist for map visibility without assigning a callsign to a responder
- Local KISS TCP input for an existing Dire Wolf or compatible TNC
- Optional bundled Dire Wolf sound-card decoding and receive-only RF-to-IS iGate
- In-app Dire Wolf detection, Mac audio input/output selection, decoder health,
  recent activity, and audio-level reporting without Terminal
- Standard, compressed, and Mic-E APRS position decoding
- Live APRS position freshness, telemetry, and map trails
- Main-page APRS-IS health with last-packet, receive/decode, update, and
  reconnect counters
- Optional nearby APRS station map feed centered on the configured map home
- Immediate APRS filter refresh when tracked responders are saved
- One-minute responder-position reconciliation alongside live APRS updates
- Facility status and capacity tracking
- Reusable saved locations for staging areas, access points, landmarks, and
  other operational places
- Incident action notes and a complete activity timeline
- Live browser updates using Server-Sent Events
- Online OpenStreetMap tiles with a usable offline grid fallback
- Host-cached map tiles and bounded downloadable offline map packs
- Optional address search through OpenStreetMap Nominatim
- Automatic one-time address resolution when an incident or facility is saved
  without coordinates, with a visible warning for incidents that remain unmapped
- Address-based map home configuration and facility/location geocoding
- Persistent KML overlays for points, routes, tracks, and polygon boundaries
- Interactive map markers, lines, and areas saved as shared overlays
- Click-to-click driving routes through a default or local OSRM-compatible router
- Persistent vehicles and equipment inventory with readiness and maintenance status
- Shared operations schedule for shifts, exercises, training, maintenance, and meetings
- Phone-focused trusted-LAN responder companion with one-time QR enrollment,
  revocable device credentials, status updates, current assignment, and active
  incident awareness
- Responder qualification tracking with expiration and readiness warnings
- Append-only local/LAN operational messaging with incident-linked channels
- Explicit one-time browser device-location capture for field responders
- Authenticated OwnTracks HTTP location ingest for Host-mode field phones
- Large full-width Situation map with smooth drag panning, pointer-centered
  wheel/trackpad zoom, keyboard controls, an expandable view, and a detachable
  map window with a full-screen control
- Dark and light operator themes
- Search, filters, keyboard shortcuts, and responsive layouts
- In-app graceful quit control that stops APRS and the local server
- Background-utility Mac behavior so the browser-based app does not linger or
  bounce in the Dock
- Append-only, sync-on-write local event log
- Automatic daily event-log snapshots with bounded 14-copy retention
- One-click event-log backup download
- Operator-reviewed Winlink incident summaries and ICS-213, ICS-214, and
  ICS-309 exports, plus `//WL2K`-prefixed `.eml` handoff to an external mail
  client with no automatic transmission
- Portable Windows, Apple-silicon Mac, and Intel Mac packages
- No runtime, database, cloud account, Docker, or administrator install

## Run it

### Windows

1. [Download the Windows x64 application](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.15/Tickets-Local-0.5.15-Windows-x64.exe), or download and unzip the [portable Windows ZIP](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.15/Tickets-Local-0.5.15-Windows-x64-unsigned.zip).
2. Double-click the downloaded `.exe` or `Tickets Local.exe` inside the ZIP.
3. The console opens in the default browser.
4. Keep the executable running while using the application.

Windows may show a SmartScreen notice until release binaries are code-signed.
Choose **More info** only after verifying the checksum and source.

### macOS

1. [Download the signed and Apple-notarized universal DMG](https://github.com/andrewgossett/tickets-local/releases/download/v0.5.16/Tickets-Local-0.5.16-macOS-universal-notarized.dmg).
2. Open the downloaded DMG.
3. Drag **Tickets Local.app** into **Applications**.
4. Open **Tickets Local** from Applications.

The universal package supports both Apple silicon and Intel Macs. Its Developer
ID signature and Apple notarization can be verified against the checksum shown
on the [release page](https://github.com/andrewgossett/tickets-local/releases/tag/v0.5.16).

For a Developer ID owner, the project includes a universal Mac signing kit.
Build the kit with:

```bash
./scripts/package-macos-signing-kit.sh
```

The resulting kit assembles Intel and Apple-silicon binaries into one app,
signs the app and DMG with the Developer ID Application certificate in the
Mac Keychain, notarizes through the saved `TicketsLocal-Notary` profile, staples
Apple's ticket, and verifies the finished DMG with Gatekeeper. The certificate
private key and notarization credentials remain in the local Keychain.

For the optional built-in Local RF decoder, install Dire Wolf on the release
Mac with `brew install direwolf` before running the signing tool. The tool copies
the decoder and its non-system libraries into the signed app; end users do not
need Homebrew. The app remains universal, while the built-in decoder is
available on each architecture supplied by the release Mac. Internet-only and
Existing KISS TNC modes remain available on either architecture.

### Build from source

Go is the only development dependency:

```bash
go test ./...
go run . --no-browser
```

To build all portable packages on macOS or Linux:

```bash
./scripts/package-release.sh
```

On Windows, build and test the x64 GUI package with PowerShell:

```powershell
.\scripts\package-windows.ps1
& '.\dist\Tickets-Local-Windows-x64\Tickets Local.exe'
```

The Windows script runs the complete Go suite and `go vet`, then produces
`dist/Tickets-Local-0.5.16-Windows-x64-unsigned.zip` and its `.sha256` file.
It stages only the executable, README, license, and notice files in a fresh directory.
Use `-GoExecutable 'C:\path\to\go.exe'` when Go is not on PATH.
Run `./scripts/test-windows-package.ps1` in PowerShell 7 to also verify the PE
architecture, ZIP contents, checksum, and failure handling used by Windows CI.
The script deliberately creates unsigned packages. Signed distribution requires
a trusted Authenticode certificate with its private key in a secure credential
store, or an authorized Microsoft Artifact Signing profile, plus Windows SDK
SignTool. Never place signing credentials in this repository.

Commit reviewed changes before running the macOS/Linux release script: its
source ZIP uses `git archive HEAD` with explicit private-data exclusions, so
binaries and corresponding source must come from the same clean checkout.
Runtime data must remain outside the checkout. Windows data files inherit the
data directory's Windows access controls; Go's Unix `0600` bits do not set
Windows ACLs. Use the default per-user application-data directory, or a custom
directory already restricted to the intended operator.

Windows flushes snapshot and restore file contents before publishing them;
directory `fsync` is retained on Unix and omitted on Windows, where it is not
supported. This does not provide an additional Windows directory-metadata
durability guarantee on sudden power loss.

Standalone and Client modes listen only on `127.0.0.1:8787`. Host mode listens
on the configured private-LAN port and requires the generated LAN access key
for remote operational API calls. If a copy is already running, a second launch
opens the existing local session. Use **Quit Tickets Local** in the sidebar
when finished; closing the browser alone does not stop the application process.

## Multiple computers on one LAN

Use the same 0.5.x application version on every computer.

1. On the computer that will remain running, open **Settings → Shared
   operations**, select **Host**, save, copy the generated access key, and use
   **Quit to apply**. Reopen Tickets Local.
2. The Host displays one or more LAN addresses such as
   `http://192.168.1.50:8787`. Copy the address that belongs to the private
   network used by the Client computers.
3. On each other computer, choose **Client**, enter the Host address and access
   key, select **Test host connection**, save, quit, and reopen.
4. The Host remains the only authority for incidents, responders, facilities,
   locations, overlays, settings, APRS, tracks, activity, and backups. Client
   installations forward changes immediately, receive live events, and poll
   every ten seconds as a fallback.

Role settings are local to each installation and stored separately from the
operational event log. If the Host is unavailable, Clients preserve their local
configuration but cannot read or change shared operational data. Host mode is
designed for a trusted private LAN; do not expose its port directly to the
Internet.

### Mobile responder companion

On the Host, open **Settings → Shared operations → Enroll a responder phone**.
Choose the responder and the LAN address reachable by the phone, then create
the QR code. Scan it with the phone camera while connected to the same trusted
network. The one-time code expires after 10 minutes and is removed as soon as
it is used. The phone receives its own credential and is permanently bound to
the selected responder; the shared Host key is not placed in the QR code.

The Host lists enrolled phones and their last-use time in the same Settings
panel. **Revoke** immediately disables one phone without changing other Clients
or enrolled devices. Device secrets are stored only as hashes in the
owner-readable `mobile-devices.json`, separate from the operational event log
and backups. Manual shared-key entry remains available for compatibility.
Keep the Host on a trusted private LAN and do not expose it directly to the
Internet. Offline mobile queuing remains planned.

## Repeater and MeshCore map feeds

Under **Settings → Operational integrations**, the Host can read separate
amateur and GMRS GeoJSON point feeds. Common properties such as `callsign`,
`name`, `frequency`, `output_frequency`, `offset`, `tone`, `ctcss`, `mode`,
`access`, `source`, and `details` are normalized into map labels. Configure only
an export that your organization is authorized to use; Tickets Local does not
scrape or bypass access controls on repeater-directory services.

A local MeshCore companion or gateway can publish this read-only format:

```json
{"version":1,"nodes":[{"id":"node-id","name":"Hilltop","type":"repeater","latitude":35.9,"longitude":-86.6,"last_seen_at":"2026-09-09T14:00:00Z","snr":8.5,"battery_percent":92,"details":"Local advert"}]}
```

Responses are capped at 1,000 points and 5 MB. Invalid coordinates and telemetry
are ignored. These feeds are transient and cached by the Host; they do not enter
the append-only event log. No radio or MeshCore transmission path is included.

## APRS tracking

1. Add or edit a responder, enter the exact APRS callsign with SSID, and enable
   **Track this callsign with APRS**.
2. Open **Settings → APRS tracking** and choose **Internet only**, **Local RF
   only**, or **Hybrid**.
3. For Internet or iGate operation, enter the APRS-IS login callsign and
   numeric passcode. The default server is `rotate.aprs2.net:14580`.
4. For Local RF, select the built-in Dire Wolf decoder and radio audio input,
   or enter the KISS TCP address of an existing TNC.
5. Tickets Local automatically
   builds a buddy-list filter for the tracked responder callsigns.
6. The Situation page reports Internet and local RF health, last-packet times,
   and separate receive/decode/update counters.

To display nearby APRS activity, enable **Show nearby APRS activity on the
map** and choose a radius under APRS settings. The range is centered on the
configured Situation map home. Tickets Local adds the APRS-IS `r/lat/lon/km`
filter and plots the latest position for stations heard within the radius.
This option can operate without any tracked responders. Nearby positions are
transient, retained in memory for up to two hours, and are not added to the
operational event log.

Internet-only mode validates the APRS-IS server's verified login response and
does not transmit packets. Local RF can optionally gate eligible radio-heard
packets to APRS-IS using Dire Wolf. That iGate option is off by default and does
not enable APRS-IS-to-radio transmission, beaconing, digipeating, messages, or
objects. The verified APRS-IS connection must be live before the bundled iGate
starts.
The APRS-IS passcode is stored in a separate owner-readable local file and is
excluded from API responses, the append-only event log, and downloadable
backups. APRS-IS uses a plain TCP login, so the passcode is sent to the
configured APRS-IS server as required by the protocol. Internet access is
required for APRS-IS and online map tiles. Core dispatch records continue to
work offline.

## Operator workflow

Each active incident has a **Next Stage** button for the normal
New → Assigned → En route → On scene → Closed progression. The incident editor
also shows all five stages as selectable buttons. Closing an incident asks for
confirmation, releases its active responders, and clears its assignments.

Active incidents with no saved change or action note are highlighted on the
Situation page, in the incident tables, and on the map. The alert interval is
configured under **Settings → Organization & map home** and defaults to 10
minutes.

Select **Group by type** on the Situation resource board to display responder
type tabs. The choice remains on that browser. Select **Open map window** to
detach the map; the new window includes a **Full screen** button because
browsers require a user click before entering true full-screen mode.

For larger incidents, use **Incident command** in the incident editor to assign
the Incident Commander, command staff, and section chiefs. Each ICS role and
responder can appear only once. Command assignments are saved with the incident
and remain intact when older API clients update other incident fields.

## Data and backup

Operational data is stored as `events.ndjson`:

| Platform | Default location |
|---|---|
| Windows | `%AppData%\Tickets Local\events.ndjson` |
| macOS | `~/Library/Application Support/Tickets Local/events.ndjson` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/Tickets Local/events.ndjson` |

Each accepted change is appended and synchronized to disk before the API reports
success. Use **Settings → Complete backup** to save a ZIP containing the
replayable NDJSON event stream and every incident attachment. **Event log only**
retains the prior NDJSON-only download for replay and diagnostics.

Use **Settings → Validate & restore event log** to dry-run an Event log only
`.ndjson` backup. The app reports record counts without changing data. Restore
is enabled only for that validated file and asks for confirmation; applying it
first preserves the current log under `snapshots/pre-restore-*.ndjson`, then
atomically activates and reloads the imported log. Event-log restore does not
import attachment bytes; use and retain Complete backups for attachment files.

Each computer's LAN role, Host address, and LAN access key are stored separately
in owner-readable `network.json` beside the event log. That file and the APRS-IS
passcode file are excluded from downloadable operational backups.

Standalone and Host installations keep automatic timestamped copies under the
`snapshots` directory beside `events.ndjson`. A snapshot is made after a valid
log is loaded and, during long-running operation, when at least 24 hours have
passed and the log has changed. The newest 14 snapshots are retained. Client
installations do not snapshot their non-authoritative local data. Snapshots
contain only the operational event stream; APRS credentials and LAN keys remain
excluded.

Command-line overrides are intended for development:

```text
--port 8787
--data-dir /path/to/data
--no-browser
--version
```

Do not start multiple processes against the same custom data directory.

## KML overlays

Open **Settings → KML map overlays** to import a `.kml` file. Imported geometry
is normalized into the local event log and remains available after restart.
Points, line strings, polygons (including inner rings), and Google `gx:Track`
geometry are supported. Imports are limited to 5 MB, 2,000 features, and 50,000
coordinates. Compressed `.kmz` files are not supported.

## Offline maps, drawing, and routing

Every map tile viewed through Tickets Local is cached by the authoritative
Standalone or Host installation. Use **Settings → Map packs** to preload a
bounded radius around the configured map home. Cached tiles continue to work
without Internet access and are shared with Client installations.

Use **Draw** on the Situation map to save markers, lines, and areas. Drawings
use the same persistent, backed-up overlay model as KML. Use **Route**, then
select two map points, to request a driving route. Routing is optional and
fails without affecting CAD operation when the default Internet router or a
configured `TICKETS_LOCAL_ROUTE_URL` service is unavailable.

## Vehicles and equipment

Open **Assets** to track vehicles and equipment, identifiers, quantities,
readiness, current location, custodian, maintenance dates, and notes. Asset
changes are Host-authoritative, conflict protected, audited, replayable, and
included in event-log backups.

## Operations schedule

Open **Schedule** to plan shifts, exercises, training, maintenance windows,
meetings, and other operational work. Items include local start/end times,
location, coordinator, assigned responders, status, and notes. Schedule changes
are Host-authoritative, conflict protected, audited, replayable, and backed up.

## Personnel qualifications

Open **Qualifications** to record responder certifications, courses, licenses,
and exercise participation. Records can include the issuing provider,
credential ID, completion and expiration dates, status, and notes. The page
highlights credentials expiring within 60 days and those already expired.

## Operational messages

Open **Messages** to post general, command, operations, logistics, or medical
coordination updates. Messages may be linked to an incident and include an
operator name or call sign. They are Host-authoritative and append-only inside
Tickets Local; this feature never sends traffic to email, SMS, or cloud chat.

## Device location

In a responder record, select **Use this device’s location** to capture one
position from the browser. Existing responders are updated through a narrow,
conflict-protected position endpoint; new responders keep the coordinates when
the record is saved. Tickets Local does not request continuous or background
browser tracking.

For repeated phone updates, save the responder and copy its **OwnTracks HTTP
endpoint** from the responder editor. In OwnTracks HTTP mode, use any Basic Auth
username and the Tickets Local Host LAN key as the password. Location reports
update the responder and map trail. This is for a trusted private LAN; do not
expose the Host or an unencrypted HTTP endpoint to the public Internet.

For GPS trackers or gateways that support OpenGTS `gprmc`-style HTTP updates,
copy the responder's **OpenGTS-compatible HTTP endpoint**. Send `lat` and `lon`,
with optional `date`, `time`, `speed`, `head`, and `alt` parameters. Speed is
interpreted as km/h and altitude as meters. Use any Basic Auth username and the
Host LAN key as the password. Keep this plaintext HTTP integration on a trusted
private LAN or place TLS termination in front of it.

## Deliberate v0.5 boundary

The original Tickets CAD system includes a much broader feature set. This local
release does not yet include:

- Named user accounts, per-user passwords, RBAC, or multi-organization isolation
- TLS termination, Internet-facing hosting, or offline multi-master data merge
- Patient care records and protected medical information
- External SMS, Slack, Zello, Meshtastic, DMR, or webhook delivery
- Vendor-specific binary GPS tracker protocols
- APRS radio transmission, messaging, objects, telemetry dashboards, or
  Internet-to-radio iGate operation
- Automated course delivery and learning-management-system integration
- Legacy MySQL import and event-log restore UI

Those features should be added as versioned modules around the domain/event API,
without changing the one-binary install model. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the extension approach.

## Keyboard shortcuts

- `N` — new incident when focus is not in a field
- `Ctrl+K` / `Cmd+K` — focus search
- `Ctrl+Enter` / `Cmd+Enter` — add an incident action note

## License and attribution

Tickets Local is distributed under GNU GPL v2. See [LICENSE](LICENSE) and
[NOTICE.md](NOTICE.md). OpenStreetMap tiles and geocoding are used only when the
operator requests online mapping functionality; follow the relevant provider
usage policies for production deployments.
