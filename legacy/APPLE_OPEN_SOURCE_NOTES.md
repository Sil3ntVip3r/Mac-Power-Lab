# Apple Open Source Reference Notes

Useful public pages:
- https://opensource.apple.com/releases/
- https://opensource.apple.com/projects/

How this helps MacPowerLab:
- Use Apple's public/open source releases and projects as a source map.
- Look for public IOKit, power management, powermetrics-adjacent, Swift/MLX/Metal-related references.
- Treat these as references only. Apple Silicon SMC, private AppleSmartBattery keys, and some powermetrics internals may not be public.

Useful project areas to watch:
- Swift / system tooling
- MLX for Apple Silicon workload ideas
- Container / Virtualization for controlled workload concepts
- LLVM/Clang for compiler workload generation
- WebKit for browser-style workload references

MacPowerLab rule:
Power monitoring remains the main focus. System monitoring features should support the power story:
- what caused the load
- what power source supplied it
- how the charger/battery reacted
- whether thermal/performance limits affected the result
