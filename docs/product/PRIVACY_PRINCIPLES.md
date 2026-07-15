# Privacy Principles

## Default behavior

MacPowerLab is local-only by default. It does not upload telemetry, expose a
non-loopback API, or require a cloud account.

## Prohibited by default

Future shared data must not include raw usernames, hostnames, serial numbers,
email addresses, full paths, process command lines, raw logs, or precise
identifiers that allow a device or person to be reconstructed.

## Requirements before community telemetry

1. Explicit opt-in with versioned consent.
2. Preview of the exact already-anonymized outgoing payload.
3. A documented purpose and retention policy for every field.
4. Separate controls for device, battery, charger, benchmark, and application
   categories.
5. Revocation and deletion controls.
6. Local record of submissions and schema versions.
7. Threat modelling for re-identification through rare hardware, chargers,
   precise timestamps, longitudinal fingerprints, and app lists.
8. Application identifiers are excluded by default or mapped to broad public
   categories unless the user grants a separate explicit opt-in.

No network telemetry implementation may precede this design and review.
