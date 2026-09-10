# Changelog

## Unreleased

- Added Host-generated phone QR enrollment with 10-minute, single-use tokens.
  QR codes contain no shared LAN key; each phone receives a random credential
  bound to one responder and can access only the scoped mobile state and status
  routes.
- Added Host-side enrolled-phone visibility, last-use timestamps, and immediate
  per-device revocation. Device secrets are stored only as hashes in a separate
  owner-readable file excluded from the operational event log and backups.
- Added the first phone-focused mobile companion at `/?view=mobile`. A responder
  can connect to a trusted-LAN Host with the existing access key, select their
  record, update operational status with conflict protection, and see their
  current assignment and the active incident list.

- Displayed the running release version prominently on both Settings and
  About & Licenses.
- Kept local Codex skills, agent instructions, handoff material, and the
  development backlog out of future public source commits.

- Added a plainly visible About & Licenses page identifying Tickets Local as
  an AI-assisted hobby project, disclaiming warranties, and warning operators
  that it is not an official or certified replacement for emergency systems.
- Added consolidated third-party license and attribution notices, linked
  OpenStreetMap/ODbL attribution, and pinned matching source archives for every
  library bundled with Dire Wolf in the signed macOS app.

- Fixed intermittent Linux CI failures in automatic snapshot verification by
  comparing the append-only event log with its latest snapshot instead of
  relying on filesystem timestamp precision.
- Added a per-computer Situation map position selector so operators can place
  the map below the summary, below the overview cards, or in its standard
  location below dispatch and resources without changing shared LAN data.
- Color-coded river-gauge cards and map markers by the most severe current or
  forecast category: blue normal, yellow action, orange minor, red moderate,
  and purple major flood stage.
- Made saved drawing deletion discoverable from the Situation map with a
  persistent drawing count, click-to-delete guidance, a Manage drawings
  shortcut, and clearly labeled Delete buttons in Settings.
- Prevented release packaging and repeated notarization from accumulating old
  DMGs or timestamped DMG backups in `dist`; the current signed build is kept.
- Made saved map drawings directly selectable. Operators can click a drawn
  marker, line, or polygon on the map and confirm its deletion without opening
  Settings; imported KML overlays remain protected from accidental map clicks.
- Added incident cross-street lookup for `&`, `and`, `at`, `/`, and `@`
  formats. When ordinary address search has no match, Tickets Local finds the
  shared OpenStreetMap road node near the configured operating area.
- Added a nearby USGS gauge finder to Water settings. Operators can search by
  the configured map home and radius, choose stations by name and distance,
  and add their IDs without researching station numbers separately.
- Allowed the browser object URLs used by proxied NOAA radar images in the
  Content Security Policy. Radar requests could previously return valid storm
  imagery and update the timestamp while the browser silently rendered a 0×0
  blocked image.
- Fixed the map drawing and routing toolbar overlapping the temperature and
  weather badge. The drawing controls now occupy a separate responsive row and
  wrap safely on narrow map windows.
- Fixed NOAA radar images loading successfully but being hidden by browser
  compositing. Base tiles, radar, routes, and markers now use an explicit map
  layer order while markers retain normal pointer interaction.
- Stopped treating NOAA's current-only public radar mosaic as a time-enabled
  animation source. Map changes now clear the old, misaligned raster and load
  one current frame immediately; multi-frame animation remains available for a
  configured time-aware local radar endpoint.
- Added an always-visible map status while the radar layer is enabled, showing
  its observation date and time, loading state, cached-data warning, or failure.
- Prevented routine screen refreshes from repeatedly replacing a slow in-flight
  radar request, which could otherwise leave the visible layer loading forever.
- Restored reliable hidden states across map and settings controls, made the
  APRS layer toggle hide trails as well as markers, fixed the offline map grid
  beneath tiles, and rendered polygon holes with the even-odd fill rule.
- Preserved active warning geometry across routine state polling so the weather
  card and its map polygon remain synchronized between weather refreshes.

- Preserved unsaved LAN settings while navigating or while status polling runs,
  added refreshable detected Host addresses, and made a successful Client
  connection test save the settings. Same-role Client address/key changes and
  Host key rotation now apply immediately without restarting; role and listener
  port changes retain explicit restart guidance.
- Preserved unsaved organization/map, APRS, weather, water, and integration
  drafts across background refreshes and page navigation. Removed a duplicate
  Messages submit binding that could post the same operator message twice.

- Fixed Windows snapshot creation, retention, and event-log restore failing on
  unsupported directory synchronization; file contents are still flushed before
  publication. Added directory-validation regression coverage.
- Made the Dire Wolf audio discovery test use a native child process on all
  platforms and limited POSIX permission assertions to non-Windows systems.
- Added native PowerShell Windows x64 GUI packaging with full tests, static
  analysis, a fresh allowlisted staging directory, explicit unsigned labeling,
  and a SHA-256 checksum; added a Windows CI build.
- Restricted source ZIPs to reviewed Git content with tested exclusions for
  runtime data and credentials, ignored the actual `aprs-passcode` filename,
  and fixed the CI release-checksum upload path.

- Made the Situation map's detached full-screen view a real new-window link, with screen-sized popup placement when the browser permits it, a reliable link fallback when scripted popups are restricted, and no operational-feed panels consuming space above the detached map.

- Connection monitoring now treats configured repeater and MeshCore feeds with
  zero returned map items as delayed/no-data rather than connected. Configurable
  diagnostic rows now open and highlight their relevant Settings panel.
- Added separate selectable amateur-repeater, GMRS-repeater, and MeshCore-node
  map layers. The Host reads bounded authorized/local exports, caches them for
  Clients, and never transmits to a radio or mesh.
- Added an opt-in 100-mile OpenStreetMap/Overpass amateur-repeater lookup with
  an explicit map-center disclosure and source attribution.
- Added an automatic Settings connection tester with manual refresh and
  fifteen-second checks while visible. It distinguishes connected, delayed,
  disconnected, and disabled states for the app, LAN Host and Clients,
  APRS-IS, local RF/Dire Wolf, NWS weather and radar, water providers, storm
  and road feeds, AREDN nodes, and local sensors.
- Added a permanent Host-plus-two-Clients validation covering authenticated
  shared state, distinct client presence, concurrent-edit conflicts, clear Host
  outage errors, and authoritative event-log replay after interruption.
- Added Host-owned cache-through OpenStreetMap tiles and bounded downloadable
  offline map packs centered on the configured operating area.
- Added persistent interactive map markers, lines, and areas with undo,
  validation, audit events, LAN propagation, replay, and backup behavior.
- Added click-to-click driving routes through the default OSRM service or an
  optional local/AREDN-compatible endpoint, with bounded geometry.
- Added a vehicles and equipment inventory with readiness, quantities,
  location, custodian, maintenance dates, notes, conflict protection, and
  append-only audit history.
- Added a shared operations schedule for shifts, exercises, training,
  maintenance, meetings, responder assignments, and LAN-safe status updates.
- Added responder qualification records for certifications, courses, licenses,
  exercises, providers, credential IDs, expiration warnings, and readiness history.
- Added incident-specific ICS command structures for Incident Commander,
  command staff, and section chiefs with responder validation and atomic updates.
- Added append-only internal operational messaging with channel filters,
  optional incident links, operator attribution, LAN propagation, and replay.
- Added explicit one-time browser device-location capture for responders using
  a narrow conflict-protected API with no background tracking.
- Added OwnTracks HTTP location ingest for Host-mode field phones with scoped
  Basic authentication, timestamp validation, map trails, and bounded payloads.
- Added OpenGTS-compatible gprmc HTTP location ingest for existing responders,
  including scoped LAN authentication, unit conversion, timestamp validation,
  persistent map trails, and copyable per-responder endpoints.
- Added opt-in Host-owned NWS alert monitoring with bounded responses, cached
  stale fallback, dashboard summaries, and warning polygons on the map.
- Added an opt-in NOAA radar overlay proxied and cached by the Host, with map
  timestamp metadata and adjustable opacity for Standalone and Client views.
- Added bounded APRS weather decoding for nearby position-reporting stations,
  including temperature, wind, rain, humidity, and barometric pressure.
- Added operator-configurable alert and radar endpoints for trusted AREDN or
  local-network weather services, with URL validation and Host-only fetching.
- Added optional two-to-six-frame radar animation with a map play/pause control
  and five-minute frame boundaries.
- Added opt-in, Host-owned USGS river and stream gauge monitoring for up to 25
  sites, with stage, flow, six-hour trends, observation age, provisional-data
  labels, local-source override, and cached stale fallback.
- Added current temperature, conditions, humidity, wind, and visibility from
  the nearest NWS observation station, plus a persistent on-map summary.
- Added an on-map Layers menu for independently showing radar, alerts,
  incidents, responders, facilities, saved locations, APRS, and water gauges.
- Repaired the Settings grid so weather and water cards no longer overflow to
  the right of the page.
- Reduced APRS, weather, and river status to a compact secondary feed strip on
  the Situation page, with expandable details and automatic expansion for
  severe or extreme weather alerts.
- Added optional NOAA NWPS enrichment for up to ten configured water gauges,
  including observed and forecast flood categories, minor flood stage, and
  forecast crest stage and time.
- Added Host-cached storm-report and road/infrastructure GeoJSON feeds as
  independently selectable map layers with source and observation time.
- Added read-only AREDN endpoint monitoring with reachability, latency, and
  route-cost display; the app never changes node configuration.
- Added a documented version-1 local sensor format and bounded polling for up
  to ten explicitly configured endpoints.
- Added incident JPEG, PNG, and PDF attachments with 10 MB/file and 20/file
  per-incident limits, authenticated LAN transfer, and a complete ZIP backup
  that includes both the event log and attachment files.
- Prevented the Client Host-connection test from following HTTP redirects, so
  the LAN access key is sent only to the exact Host address entered by the
  operator.
- Added editable previews and explicit downloads for Winlink incident
  summaries, ICS-213 messages, ICS-214 activity logs, and ICS-309 communications
  logs. Tickets Local does not transmit these exports or store new credentials.
- Added reviewed `.eml` handoff for Winlink-addressed email with validated
  recipients and `//WL2K R/`, `P/`, `O/`, or `Z/` subject precedence. Sending
  remains an explicit action in the operator's external mail client.
- Added automatic owner-readable snapshots of the validated append-only event
  log at startup and during long-running Host/Standalone operation. Snapshots
  are created only after changes, no more than daily, with the newest 14 kept.
- Added a Settings restore workflow with non-mutating dry-run counts, strict
  NDJSON and path validation, file-hash continuity, explicit confirmation, an
  automatic pre-restore log copy, atomic replacement, and immediate state reload.

## 0.5.0

- Added configurable Standalone, Host, and Client computer roles under
  Settings → Shared operations.
- Host mode keeps one authoritative event log and APRS service, listens on the
  selected private-LAN port, generates a shareable access key, displays usable
  LAN addresses, and reports recently active Client computers.
- Client mode keeps the browser interface on localhost while securely adding
  the configured access key to proxied host reads, writes, backups, APRS
  controls, and Server-Sent Event traffic.
- Added an in-app Host connection test, access-key copy/regeneration controls,
  connection status, ten-second Client polling fallback, and clear restart
  guidance after a role or port change.
- Added optimistic update checks for incidents, responders, facilities,
  locations, and KML overlays. A stale editor receives an HTTP 409 conflict
  instead of silently overwriting a newer change from another computer.
- Client installations no longer run their own APRS receiver; the Host owns the
  shared APRS connections, decoder, counters, tracks, and responder positions.
- LAN access keys are stored in a separate owner-readable `network.json` file
  and are excluded from the operational event log and downloadable backups.

## 0.4.1

- Added in-app detection of the bundled Dire Wolf decoder and its available
  PortAudio input and output devices.
- Replaced the manually typed audio-device setting with device selectors,
  default-device suggestions, and clear configuration validation.
- Added decoder version, architecture, audio-level, and recent startup/activity
  details to the APRS status panel.
- Added a one-click Save and Apply flow and clearer Local RF configuration
  errors, eliminating the need to run Dire Wolf setup commands in Terminal.
- Changed the Mac app to a background utility so its browser-based process no
  longer bounces or remains as a misleading Dock application.

## 0.4.0

- Added selectable Internet only, Local RF only, and Hybrid APRS modes.
- Added a local KISS TCP receiver for existing Dire Wolf, software TNC, and
  compatible hardware TNC connections.
- Added supervised bundled Dire Wolf support for direct 1200-baud radio audio
  decoding, with Mac microphone permission metadata.
- Added an explicit, optional receive-only RF-to-APRS-IS iGate setting. The app
  validates the APRS-IS login before starting it; Internet-to-radio
  transmission, beaconing, and digipeating are not enabled.
- Added AX.25 UI/KISS decoding, malformed-frame limits, local packet counters,
  local connection health, and `local_rf` audit/trail source labels.
- Hybrid mode now deduplicates radio and APRS-IS copies of the same packet even
  when the network path has changed.
- Internet-only remains the default and does not start a local decoder.
- Updated the Mac release builder to bundle installed Dire Wolf executables and
  their non-system libraries inside the signed app, while carrying the matching
  Dire Wolf 1.8.1 GPL source archive.

## 0.3.8

- Added a visible Quit Tickets Local control in the sidebar.
- Quitting now asks for confirmation, stops APRS, gracefully shuts down the
  local HTTP server, and exits the application process.
- The browser displays a clear stopped screen after shutdown so the operator
  knows it is safe to close the browser window.
- Protected the shutdown endpoint with both an explicit JSON confirmation and
  a custom same-application request header to prevent ordinary cross-site forms
  from stopping the local service.

## 0.3.7

- Added one-click Next Stage buttons for active incidents and a visual
  five-stage progression control in the incident editor.
- Added a detachable Situation map window sized to the screen, with its own
  user-initiated browser full-screen control.
- Added an optional resource-board type-tab view whose preference is retained
  on the operator's computer.
- Added visual stale-incident alerts, row highlighting, and map-marker emphasis
  after a configurable no-update interval that defaults to 10 minutes.
- Active incident updates and action notes reset the stale timer.
- Disabled embedded web-asset caching so a replaced binary cannot continue
  serving an older interface from the browser cache.

## 0.3.6

- Replaced the unverified `pass -1` APRS-IS session with a callsign and numeric
  passcode login.
- APRS now remains in an Authenticating state until the server returns a
  matching `logresp ... verified` response; unverified or mismatched responses
  are rejected and shown as authentication failures.
- Added a masked APRS-IS passcode setting and verified-login indicators on the
  Situation and Settings status panels.
- The APRS-IS passcode is stored in a separate owner-readable local file and is
  omitted from API responses, the append-only operational log, and backups.
- The verified connection remains inbound-only: Tickets Local does not transmit
  APRS packets, messages, objects, or iGate traffic.

## 0.3.5

- Added an APRS-IS health panel to the Situation page with connection state,
  server/filter details, last-packet age, packet and decoded-position counts,
  responder updates, nearby-station count, and reconnects.
- Added an optional APRS area feed centered on the configured Situation map
  home with a 1–250 mile radius.
- Nearby position-bearing APRS stations now appear as distinct map markers,
  while tracked responders retain their dedicated responder markers and trails.
- Area activity supports an area-only connection with no tracked responders,
  remains receive-only with `pass -1`, and uses the APRS-IS server-side range
  filter.
- Nearby station positions are bounded, retained in memory for up to two hours,
  and excluded from the append-only operational event log.

## 0.3.4

- Incident and facility saves now make a one-time address lookup when coordinates are blank.
- Newly mapped incidents and facilities automatically center the Situation map on their marker.
- Active incidents without coordinates now appear in a visible map warning panel instead of silently disappearing.
- Facility lookup results now clearly require a selection and copy the selected standardized address into the facility record.
- Editing an address clears stale coordinates so the saved marker cannot remain at the old location.
- Improved geocoder identification, error reporting, and result relevance near the configured map center.
- Reversed mouse-wheel and trackpad zoom direction to match the requested macOS behavior.

## 0.3.3

- Enlarged the Situation map and made it span the full dashboard width.
- Added clearly labeled expand and collapse controls for a near-full-screen map.
- Reworked drag panning to reuse map tiles instead of rebuilding them during every pointer movement.
- Added smoother trackpad and mouse-wheel zoom centered on the pointer location.
- Allowed panning to begin over a responder, incident, facility, or saved-location marker without accidentally opening it.
- Increased map control sizes and added Escape-key support for closing the expanded view.

## 0.3.2

- Added an original Tickets Local application icon for macOS, Windows, and the browser interface.
- Embedded the icon into the signed universal Mac app, DMG release, Finder metadata, Dock, Windows executable, and browser tab.

## 0.3.1

- Added mouse-wheel zoom, drag panning, double-click zoom, keyboard map
  navigation, a home-view control, and clearer Situation map guidance.
- Added a one-minute responder-position reconciliation and map redraw while the
  continuous APRS-IS stream remains connected.
- Responder saves now explicitly request an immediate APRS connection/filter
  refresh.
- Changing or enabling a responder's tracked APRS identity now clears the prior
  cached position and trail so the map waits for the correct callsign.
- Stale-position styling is now reevaluated at least once per minute.

## 0.3.0

- Added reusable saved locations with distinct record identities, address
  search, map markers, editing, deletion, and quick reuse on incidents and
  facilities.
- Added address-based map-home configuration while retaining resolved
  coordinates for offline startup.
- Added persistent KML imports for points, routes, polygons, and `gx:Track`
  geometry, including visibility, color, zoom-to-overlay, and deletion.
- Added facility address geocoding.
- Applied protected create-versus-edit identity handling to incident, facility,
  and location forms.
- Added KML safety limits and regression coverage for map-record replay and API
  lifecycle behavior.

## 0.2.1

- Fixed Add responder retaining a previous responder's edit identity and
  overwriting that record.
- Added store and API regression coverage confirming that multiple responders
  receive distinct IDs and remain present after event-log replay.
- Updated the temporary unsigned Mac preparation tool to clear the app's
  quarantine flag before applying its local signature.

## 0.2.0

- Added receive-only APRS-IS responder tracking, position decoding, live map
  updates, freshness indicators, and trails.
