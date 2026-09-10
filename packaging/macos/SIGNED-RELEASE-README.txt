TICKETS LOCAL 0.5.9 — MAC SIGNING AND NOTARIZATION KIT
======================================================

This kit creates one universal Tickets Local app that runs on both Intel and
Apple-silicon Macs. It signs the app and DMG with your Developer ID Application
certificate, submits the DMG to Apple, staples the notarization ticket, and
verifies Gatekeeper acceptance.

PREREQUISITES
-------------

1. A valid "Developer ID Application" certificate and its private key under
   Keychain Access > My Certificates.
2. A validated notarytool Keychain profile named:

      TicketsLocal-Notary

3. Xcode or current Xcode Command Line Tools.
4. Internet access while the notarization submission runs.
5. For the built-in Local RF option, Homebrew and Dire Wolf:

      brew install direwolf

   The signing tool can run this Homebrew command for you when Homebrew is
   already installed. It then copies Dire Wolf and its non-system libraries
   into the signed app. End users do not need Homebrew.

RUN THE KIT
-----------

1. Keep this entire folder together.
2. Double-click "Sign and Notarize Tickets Local.command".
3. If macOS blocks the command file itself, Control-click it and choose Open.
4. Approve Keychain access if macOS asks to use the Developer ID private key.
5. Wait for Apple to return "Accepted".

The finished files appear in the "Signed Release" folder:

  Tickets-Local-0.5.9-macOS-universal-notarized.dmg
  Tickets-Local-0.5.9-macOS-universal-notarized-SHA256.txt
  Tickets-Local-0.5.9-source.zip
  Third-Party-Source/ (six matching source archives)

The process can take several minutes. Do not close Terminal while Apple is
processing the submission.

WHAT END USERS DO
-----------------

1. Open the notarized DMG.
2. Drag Tickets Local to Applications.
3. Open Tickets Local normally.

End users should not need the old preparation command, Terminal commands,
quarantine removal, or "Open Anyway" workaround.

SECURITY
--------

The script uses the signing certificate and private key directly from your Mac
Keychain. It uses the saved TicketsLocal-Notary profile for Apple
authentication. It does not display, copy, export, or package either secret.

The app and DMG are built in a temporary folder, and the temporary folder is
removed when the process exits.

LICENSE AND SOURCE
------------------

Tickets Local is distributed under GNU GPL v2. The signing tool places the
matching Tickets-Local-0.5.9-source.zip and Third-Party-Source directory in the
Signed Release folder. Distribute both archives alongside a DMG that contains
Dire Wolf, or provide an equivalent durable source-code offer.
