# Security Policy

## Supported versions

tertib is pre-1.0 and ships from `main`. Security fixes land on the latest
release and the `main` branch.

| Version             | Supported |
|---------------------|-----------|
| Latest release      | ✅        |
| `main`              | ✅        |
| Older tagged builds | ❌        |

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Report privately through GitHub:

1. Go to the repository's **Security** tab → **Advisories** → **Report a vulnerability**
   (direct link: https://github.com/chalvinwz/tertib/security/advisories/new).
2. Describe the issue, affected version (`tertib version`), and steps to reproduce.

This routes the report privately to the maintainer via GitHub Private
Vulnerability Reporting — no public disclosure until a fix is ready.

## What to expect

tertib is maintained by a single person on a best-effort basis. You can expect
an initial acknowledgement within a few days. Once a fix is ready it will be
released as a new tag, and the advisory will be published crediting the reporter
(unless you prefer to remain anonymous).

## Scope

tertib reads your repository, resolves secrets, and sends code to a model
endpoint you configure. In-scope reports include:

- **Secret handling** — a resolved API key or webhook leaking into logs, error
  messages, or report output (tertib registers every resolved secret for
  redaction; a leak is a bug).
- **Code exfiltration** — tertib sending repository content anywhere other than
  the `base_url` you configured.
- **Injection** — repository content (file names, code, diffs) escaping its role
  as data and causing tertib to take unintended actions.

Note that tertib sends the code under review to the model endpoint in your
config. Choosing that endpoint (including whether it is self-hosted) is your
responsibility. Vulnerabilities in AWS, your model provider, or your CI system
should be reported to those vendors.
