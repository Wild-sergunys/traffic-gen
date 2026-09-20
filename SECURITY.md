# Security Policy

## Supported Versions

The project is under active development. Only the latest release
is supported with security fixes.

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Instead, send an email to:

    qupqy@mail.ru

You will receive an acknowledgment within 72 hours. If the report is
valid, a fix will be prepared and released as soon as reasonably
possible. You will be credited in the release notes unless you prefer
to stay anonymous.

## What to include

A good report usually contains:

- A short description of the vulnerability and its impact.
- Steps to reproduce, ideally with a minimal example.
- The version of traffic-gen you are running (`./bin/traffic-gen --version`).
- Any relevant logs, packet captures, or configuration files.
- If you have one, a suggested fix or mitigation.

## Scope

This policy covers vulnerabilities in the traffic-gen tool itself —
for example, remote code execution through crafted scenario files,
denial of service triggered by malformed input, or bugs that could
cause the tool to send traffic to unintended destinations.

It does not cover:

- Vulnerabilities in third-party dependencies. Report those upstream
  to the relevant project.
- Misuse of the tool. Running traffic-gen against systems you do not
  own or have permission to test is illegal, and the project takes
  no responsibility for it. See the Legal section below.

## Legal

traffic-gen is designed for testing infrastructure you own or have
written permission to test. Any other use is illegal.

If you discover that traffic-gen is being used against systems
without authorization, please report it to the relevant authorities.
This project cannot and will not assist with unauthorized testing.

## Acknowledgement

Thanks to everyone who takes the time to responsibly disclose
security issues. Your reports make the tool safer for everyone.
