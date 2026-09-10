# Tickets Local Privacy Policy

Effective date: September 10, 2026

## Scope and responsibility

This policy describes the Tickets Local application distributed from
[andrewgossett/tickets-local](https://github.com/andrewgossett/tickets-local).
Tickets Local is free, open-source software that runs on the operator's computer
or a host selected by the operator. The person or organization operating an
installation controls its records, access, configuration, backups, and retention.
This policy does not cover independently modified versions or third-party services.

## Information processed and stored

Tickets Local processes information entered, imported, or received through enabled
features. This can include incidents and notes, names and callsigns, responder
status and qualifications, addresses and coordinates, location tracks, facilities,
resources, schedules, messages, attachments, and operational activity history.

Operational records are stored in an append-only event log in the installation's
data directory. Attachments, snapshots, integration caches, downloaded map tiles,
configuration, and diagnostic logs may also be stored locally. Default directories
are:

- Windows: `%AppData%\Tickets Local`
- macOS: `~/Library/Application Support/Tickets Local`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/Tickets Local`

An operator can choose a different data directory. Browser local storage remembers
interface preferences such as theme and map layers. LAN keys and APRS credentials
are stored separately from the operational event log; separation does not mean
the files are encrypted.

The application does not include advertising, usage analytics, or automatic crash
report uploads to the project maintainer. Installing or running it does not
automatically send your operational database to the maintainer.

## Local network sharing

Standalone mode serves the interface on the local computer. In Client mode, the
application sends requests and operational changes to the configured Host and
receives shared records from it. The Host stores the authoritative shared records.
People with access to that installation and its LAN key can access shared data;
the application does not provide separate user accounts or per-user permissions.

Host communication uses HTTP and a shared key on a trusted private LAN. It does
not provide built-in transport encryption or Internet-facing hosting protection.
Operating-system access controls, network access, and any additional protection
are the operator's responsibility.

## Online features and external recipients

Online features contact their configured providers, and some refresh
automatically after being enabled or when associated views are used. Providers
can receive the connecting computer's or Host's IP address, request timing,
application identification, and information required by the feature:

- **Maps and address lookup:** OpenStreetMap tile services receive requested map
  tile coordinates. Nominatim receives address searches. Saving an address
  without coordinates can trigger automatic geocoding, sending that address.
- **Routing:** The configured routing service, by default the public OSRM service,
  receives route coordinates.
- **Weather, radar, and water information:** NWS/NOAA and USGS services receive
  location, map-area, station, or other relevant query parameters.
- **Additional feeds:** Configured repeater, mesh, infrastructure, storm, and
  sensor services receive the requests needed to obtain their data. The default
  OpenStreetMap Overpass service may receive geographic query boundaries.
- **APRS:** An enabled APRS-IS connection sends the configured callsign, passcode,
  and station or geographic filters to the APRS-IS server over plain TCP.
  Optional RF-to-Internet iGate operation can forward eligible received radio
  packets, including callsigns and positions, to APRS-IS. This is off by default;
  forwarded information may become publicly available through the APRS network.

Browser device-location capture is requested through an explicit operator action
and browser permission. A captured location is stored as operational information
locally or on the configured Host. Configured tracking integrations can also
receive location updates from external devices.

External services operate under their own policies. Using a local provider where
supported, disabling integrations, or working offline can reduce external
requests. Core local record handling does not require a project-operated cloud
account.

## Exports, backups, and retention

Exports and downloaded backups contain operational information. Complete backups
include incident attachments. Operational backups exclude the separately stored
LAN key and APRS passcode; copying the entire data directory may include them.
Email handoff creates content for an external email application rather than
automatically sending it.

The event log preserves historical changes: editing or removing an item from
the interface does not necessarily erase its previous contents from the log.
Automatic event-log snapshots normally retain the newest 14 copies. Other files,
manual exports, downloaded backups, pre-restore copies, and copies held by others
can remain separately. The project maintainer cannot remotely delete these
installation records.

Operators manage retention and deletion, including backups and browser storage,
on the computers and storage services they control. Operating-system backups or
cloud-synced folders can create additional copies under those services' policies.
No automatic secure-erasure guarantee is provided.

## Support and project services

GitHub hosts the source, releases, and issue tracker under GitHub's own privacy
policy. Information you choose to submit in public issues is public. Do not post
live incident records, personal addresses, credentials, private attachments, or
unredacted logs when requesting help. A code-signing provider processes release
artifacts and maintainer account information; signing does not require uploading
operators' runtime databases.

For questions about an organization's operational records, contact that
organization. For questions about this policy, open a
[GitHub issue](https://github.com/andrewgossett/tickets-local/issues) containing
only non-sensitive information.

## Changes

This policy will be updated as application behavior changes. Updates are recorded
in this repository's history, and the effective date above identifies this version.
