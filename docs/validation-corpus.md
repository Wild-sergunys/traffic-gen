# Validation inputs, version 1

This is a small, source-informed control corpus, not a captured dataset and not
an independent benchmark of detector quality. The payloads differ from the
built-in smoke-test strings, but some attack families overlap. Changing numbers
or casing alone does not establish generalization to unseen attacks.

## Normal traffic

`examples/web-background.yaml` selects eight repeating requests. The intended
application actions are a health check, searches, documentation lookup, and
feedback submission. Inputs include an apostrophe in a name, quoted text,
comparison signs, an ampersand, percent/plus signs, and Russian text. The docs
query contains HTML/SQL terminology without an executable injection expression.
These are synthetic benign-intent inputs, not a production traffic distribution;
whether a specific alert is a false positive still depends on its rule and the
application context. There are no cookies, user journeys, or think times.

## Control attacks and provenance

Sources consulted on 2026-11-09:

- [OWASP WSTG v4.2: Testing for SQL Injection](https://wstg.owasp.org/v4.2/4-Web_Application_Security_Testing/07-Input_Validation_Testing/05-Testing_for_SQL_Injection/).
  SQL-C01/C02 adapt numeric boolean probes; SQL-C03 uses a three-column UNION
  shape with NULL constants; SQL-C04 adapts the alternative boolean expression.
  Constants and projections were changed; these are adapted test inputs, not
  reproduced database exploits.
- [OWASP XSS Filter Evasion Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/XSS_Filter_Evasion_Cheat_Sheet.html).
  XSS-C01 adapts the SVG object example; C02 uses an attribute-context breakout
  and image handler; C03 adapts the end-title example; C04 adapts the body event
  example. Casing and alert constants were changed. Applicability depends on
  reflection context and browser behavior; this generator verifies neither.

IDs are comments beside payloads in `examples/control-sqli.yaml` and
`examples/control-xss.yaml`. Their list order is stable. Each input is submitted
via GET and POST before the next input; the cycle repeats. YAML contains raw
values, and transport encoding is applied once by the request builder.

Tests check decoding, distinctness from built-in strings, and request encoding.
They do not assert that an IDS detects these inputs or that a server is vulnerable.

## Running and keeping evaluation separate

1. Adjust the targets to the authorized test server. Save the exact YAML and
   `traffic-gen --version` output with each experiment.
2. Run normal traffic alone first:
   `./bin/traffic-gen --config examples/web-background.yaml`.
3. Run `examples/control-sqli.yaml` and `examples/control-xss.yaml` separately.
   Capture traffic at the receiver to establish which attempts arrived.
4. For a mixed run, copy the `background` block from the normal example into a
   control scenario file. Point its target at the appropriate application base
   path. The scenario endpoint and the background base path are independent.
5. Keep these control files out of rule development. Freeze the detector before
   evaluating them; if results guide rule changes, they become regression data
   and a fresh held-out set is needed.

Do not equate request counters with packet capture or alert counts: a failed
attempt can still have emitted packets, multiple alerts can describe one attempt,
and a short/slow run may not complete the whole input cycle. SQLi responses do
not prove database execution, and XSS responses do not prove browser execution.
A larger externally labeled corpus is still needed for claims about real-world
recall or false-positive rates. No IDS lab infrastructure is included here.
