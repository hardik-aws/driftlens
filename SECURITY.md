# Security Policy

## Supported Versions

| Version  | Supported          |
| -------- | ------------------ |
| latest   | :white_check_mark: |
| < latest | :x:                |

Only the latest release on the `main` branch receives security updates.

## Reporting a Vulnerability

**Do not open public GitHub issues for security vulnerabilities.**

Report privately via one of:

- **GitHub Private Vulnerability Reporting** (preferred): Use the **Security** tab → **Report a vulnerability**.
- **Email**: mailtohardiks@gmail.com

Include where possible:

- Affected component, route, or file
- Steps to reproduce / PoC
- Impact assessment (data exposure, RCE, XSS, SSRF, auth bypass, etc.)
- Affected version / commit SHA
- Suggested remediation (optional)

## Response Targets

| Stage                 | Target                 |
| --------------------- | ---------------------- |
| Acknowledgement       | within 48 hours        |
| Initial assessment    | within 5 business days |
| Fix / mitigation      | severity-dependent     |

Severity is assessed using CVSS v3.1. Critical/High issues are prioritized.

## Disclosure Policy

- Coordinated disclosure. Please allow up to **90 days** before public disclosure.
- We will credit reporters in release notes unless anonymity is requested.

## Scope

In scope:

- This repository's application code (Next.js app, API routes, server actions, middleware)
- Build and deployment configuration in this repo

Out of scope:

- Vulnerabilities in third-party dependencies already tracked upstream (report to the upstream project; we still appreciate a heads-up)
- Social engineering, physical attacks, DoS/volumetric attacks
- Findings requiring privileged local access or rooted/jailbroken devices
- Automated scanner output without a demonstrated, exploitable impact
- Hosting provider infrastructure (report to the provider)

## Safe Harbor

Good-faith research that complies with this policy is authorized. We will not pursue legal action for testing that:

- Stays within scope
- Avoids privacy violations, data destruction, and service degradation
- Does not access or modify other users' data

## Security Practices

- Dependency updates via Dependabot / Renovate
- Secrets are never committed; use environment variables and a secret manager
- Report suspected exposed credentials immediately via the channels above
