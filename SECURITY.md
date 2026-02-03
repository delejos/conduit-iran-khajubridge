# Security Policy

## Supported Versions
This repository is maintained on a best-effort basis. Security fixes and improvements are applied to the current `main` branch.

## Reporting a Vulnerability
If you discover a security issue, **do not open a public GitHub issue**.

Instead, report it privately:
- Contact the repository maintainer via GitHub (private message), or
- Use GitHub’s private security advisory feature if available for this repository.

Please include:
- A clear description of the issue
- Steps to reproduce (if applicable)
- Potential impact
- Any suggested mitigation

## Sensitive Information
Do **not** include the following in issues, pull requests, discussions, or commits:
- Secrets, tokens, passwords, or private keys
- Personal data (emails, phone numbers, or identifiers tied to individuals)
- Hostnames, internal network details, or machine-specific identifiers
- Logs or configuration files from live systems

If sensitive information is accidentally committed, report it immediately so it can be removed and rotated.

## Operational Safety Notice
This repository contains scripts and configurations that may:
- Modify firewall rules
- Interact with system services
- Affect network connectivity

Always review changes carefully and test in a non-production environment before applying to live systems.

## Automated Checks
Automated tools (e.g., static analysis and secret scanning) may be used to detect common issues. These checks are advisory and do not replace manual review.
