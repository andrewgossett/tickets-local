# Windows validation — 2026-09-09

Baseline: product 0.5.0, private repository `andrewgossett/ticketscad`, main
commit `f79ebeb`. This Windows workspace initially had no checkout or Go
installation. A clean clone was fetched and fast-forward checked before edits.
The referenced parent `CODEX_HANDOFF.md` was not present; all repository
instructions, architecture, README, changelog, and integration backlog were read.

## Baseline failures and fixes

The initial complete Windows Go suite failed five tests:

- Audio discovery used an executable Unix shell fixture. It now launches the
  native test executable as a controlled decoder subprocess on every platform.
- Restore, automatic snapshots, and snapshot retention attempted directory
  `File.Sync`, which Windows rejects. File-content synchronization remains
  mandatory; directory sync remains enabled on Unix. Windows still validates
  the directory. No persistence format, network boundary, or APRS mode changed.
- The APRS credential test assumed Unix permission bits on Windows. POSIX mode
  assertions now apply only on non-Windows systems. Windows continues to inherit
  directory ACLs; this change does not claim to implement or audit custom ACLs.

## Local validation

- Go 1.27.1, Windows amd64; Git 2.53.0.windows.3.
- Complete `go test ./...` and uncached `go test -count=1 ./...`: passed.
- Complete `go test -race -count=1 ./...`: passed with CGO enabled and the
  checksum-verified LLVM-MinGW 20260908 compiler. Compilers stay outside the repo.
- `go vet ./...`, Go formatting, and Git diff checks: passed.
- PowerShell 7 and Windows PowerShell 5.1 native unsigned packaging: passed.
  The latter used process-only RemoteSigned; no persistent policy was changed.
- Windows package integration checks cover x64 GUI PE metadata, unsigned status,
  exact four-file ZIP contents, SHA-256, failure handling, and environment
  restoration. Source-archive tests exclude untracked data and even force-added
  operational/credential paths, both at the root and in nested directories.
- The GUI executable reports version 0.5.0 and serves the app at
  `http://127.0.0.1:8787`, listening only on loopback in Standalone mode.
- Real-browser checks exercised synthetic incident creation, coordinate editing,
  mapped display, stage progression, live updates, main navigation, and map zoom.
  Chrome opened the detached map at `/?view=map`; its layout omits the sidebar
  and operational feed panels. Chrome's automation session denied the Full
  screen request (`not granted`), and the app displayed its existing error
  message. Successful OS full-screen entry still requires an interactive check.
- The test instance was stopped through the protected application shutdown API.
  Synthetic runtime data was kept outside the repository and release packages.

## Signing and remaining field checks

No currently valid code-signing certificate with a private key was found in the
CurrentUser or LocalMachine personal certificate stores. Windows SDK SignTool
was not found on PATH or under the standard Windows SDK x64 installation path;
no Artifact Signing profile was supplied or identified in the build environment.
The resulting package is explicitly unsigned. A trusted signing identity or
authorized Artifact Signing profile and SignTool are the signing prerequisites.
No signing material was exported, stored in Git, or included in packages.

The existing automated Host-plus-two-Clients test passes, but this single-PC
run does not replace the backlog's real multi-computer AREDN field validation.
