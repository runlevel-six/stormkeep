# Stormkeep documentation

A live dashboard for a WeatherFlow Tempest station, beside the weewx that
archives it, in one Kubernetes pod.

This documentation follows [Diátaxis](https://diataxis.fr): each page serves
one need. There is no tutorial yet; the how-to guides are written to be
followed in order the first time.

## How-to guides — goals

- [Run it locally](how-to/run-it-locally.md) — the dashboard and a simulated
  station on your own machine, no hardware needed
- [Deploy to Kubernetes](how-to/deploy-to-kubernetes.md) — an overlay, the
  values only you know, and how to tell it worked
- [Bring an existing weewx database](how-to/bring-an-existing-weewx-database.md)
  — keep the history, fill in what was never calculated, and move CWOP
  reporting without sending everything twice
- [Cut a release](how-to/cut-a-release.md) — the tag is the release

## Reference — look it up

- [Configuration](reference/configuration.md) — every environment variable,
  and the parts of `weewx.conf` that are yours to set
- [HTTP API](reference/api.md) — what the page reads, in what units

## Explanation — why it works this way

- [Design](explanation/design.md) — the host network and what guards it, why
  weewx stays, the driver patch, time zones and units

## Something's wrong

- [Troubleshooting](explanation/troubleshooting.md) — no live readings, a 403,
  totals that reset at the wrong hour
