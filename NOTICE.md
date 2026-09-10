# Notices

Copyright © 2026 Andrew Mark Gossett and Tickets Local contributors.

Tickets Local is an AI-assisted hobby project. It is not an official OpenISES
release, certified dispatch system, emergency service, or substitute for
validated public-safety systems and procedures. It is provided without warranty
of any kind, as described more fully in the GNU GPL. Operators are responsible
for evaluating its suitability, data sources, connectivity, and outputs.

## Tickets CAD / OpenISES

Tickets Local is an independent modernization inspired by the workflows and
GPL-licensed source of Tickets CAD:

- Project site: <https://openises.sourceforge.net/>
- Source repository: <https://github.com/openises/tickets>
- Original project history and contributors are documented by the OpenISES
  project.

Tickets CAD is licensed under GNU GPL v2. Tickets Local is distributed under the
same license. "Tickets Local" is used to distinguish this local implementation;
it is not represented as an official OpenISES release.

## OpenStreetMap

Online map tiles and optional geocoding use OpenStreetMap services:

- © OpenStreetMap contributors
- <https://www.openstreetmap.org/copyright>
- Nominatim usage policy:
  <https://operations.osmfoundation.org/policies/nominatim/>

Core dispatch records remain usable without these online services.

## APRS, APRS-IS, and Dire Wolf

The APRS integration follows the APRS-IS connection and filtering
specifications published at <https://www.aprs-is.net/> and the APRS packet
formats published by the APRS Working Group/TAPR.

Signed Mac releases can aggregate the GPL-licensed Dire Wolf 1.8.1 sound-card
TNC maintained by WB2OSZ: <https://github.com/wb2osz/direwolf>. Dire Wolf
remains a separate process and is distributed under its own GPL terms. Its
unmodified matching source archive and license are included with releases that
bundle the executable. Tickets Local uses Dire Wolf's KISS TCP output for local
packet reception and can optionally enable Dire Wolf's receive-only
RF-to-APRS-IS iGate. No Internet-to-radio path is configured.

APRS is a registered trademark of Bob Bruninga.

## Bundled third-party libraries

Signed macOS releases may include unmodified gpsd/libgps, Hamlib, HIDAPI,
PortAudio, and libusb libraries required by Dire Wolf. Exact versions, license
choices, project links, and corresponding source locations are documented in
`THIRD-PARTY-NOTICES.md`, included in the application bundle.
