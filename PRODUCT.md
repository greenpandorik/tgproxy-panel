<!-- impeccable:product-schema: 1 -->
# TGProxy Panel

## Platform
web

## Product
Self-hosted administration of Telegram proxy servers, access keys, monitoring,
site templates and panel configuration. Existing API behavior and operator data
remain authoritative.

## Audience and primary tasks
Owners and administrators manage servers, distribute access and investigate
connection problems. Viewers inspect state with the existing restricted role.
The overview should answer what is running, what needs attention and where to act.

## Stack
Existing React 19, Vite, TypeScript, Tailwind, Base UI/shadcn components, Go API,
PostgreSQL. Russian and English. No framework migration.

## Confirmed requirements
The user selected Tabler as the reference, with a fluid full-width workspace.
Clear navigation, contextual help and fewer repeated metrics/actions.
Project name, logos and appearance are configurable through existing branding
profiles. Personal display preferences remain separate from shared project data.

## Operational surfaces
Telemt nodes expose WEB runtime/carrier monitoring, capability-aware policy and
lifecycle controls, stored diagnostics with JSON export, and a per-node update
manager for the panel-pinned release. Missing capabilities and unavailable readings
remain distinguishable from healthy results and numeric zero.

Node onboarding groups identity/DNS and proxy settings before the installation
command, agent connection, readiness and diagnostics. Websites offer 15 built-in
sites, previews/customization, static ZIP import and capability-gated local/private
HTTP upstream mode. These implemented surfaces do not certify every criterion in
the two specifications; see [the acceptance matrix](docs/vnext-acceptance.md).
