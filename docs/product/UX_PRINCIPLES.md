# UX Principles

## Progressive disclosure

- **Overview:** plain-language status, current load, battery/charger state, and
  actionable warnings.
- **Details:** electrical values, component power, application attribution,
  history, and benchmarks.
- **Advanced:** sensor source, confidence, cadence, collector status, raw fields,
  contracts, and diagnostics.

## Education in context

Every important metric should eventually provide an information control with:

- what it measures;
- unit and update cadence;
- sensor or estimator source;
- confidence and known caveats;
- device-relative normal guidance;
- why it may be unavailable;
- related metrics and help links.

## Visual language

- Use line/area charts for trends over time.
- Use gauges only when a defensible bounded range exists, such as battery
  percentage or charger load relative to the negotiated contract.
- Prefer stacked bars or ranked bars for component/application shares; avoid
  dense pie charts.
- Never imply false precision or universal normal ranges.

## Accessibility

Meaning must not depend on color alone. Preserve keyboard navigation, VoiceOver
labels, sufficient contrast, readable units, and reduced-motion behavior.
