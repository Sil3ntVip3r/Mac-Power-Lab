#!/usr/bin/env python3
"""Standalone entry point for MacPowerLab app-power sampling.

The integrated Mac Power Monitor automatically enables this subsystem when run
with ``--powermetrics``. This wrapper remains useful for one-shot diagnostics.
"""

from app_power_attribution import main


if __name__ == "__main__":
    raise SystemExit(main())
