# Stormkeep

A live dashboard for a WeatherFlow Tempest weather station, and the weewx that
records it, packaged to run in Kubernetes.

The Tempest hub announces every reading on the local network as a UDP
broadcast: wind every three seconds, everything else once a minute. This
project listens to those broadcasts twice, from one pod:

- **weewx** archives them to a SQLite database and, if you like, reports them
  to the Citizen Weather Observer Program (CWOP). It is stock weewx 5 with the
  community WeatherFlow UDP driver, patched to parse packets as JSON rather
  than evaluate them as Python.
- **the dashboard** shows current conditions as they arrive — the wind needle
  moves every three seconds — and charts the weewx archive over a day, a week,
  a month or a year. It reads the database and never writes it.

No cloud service is involved: everything comes from your own network.

## What the dashboard shows

- Temperature, feels-like, today's high and low
- Wind: a live compass with the last ten minutes of direction, the speed
  trace, gust and lull
- Humidity and dew point, sea-level pressure and its three-hour trend
- Rain today, in the last hour and day, this month and year, and the rate
- UV index, solar radiation and brightness; lightning strikes and distance
- Charts of all of the above, with a table view of the same numbers
- Records since the archive began

US or metric units, chosen by each viewer; light and dark themes follow the
device.

## Getting started

- To deploy it: [Deploy to Kubernetes](docs/how-to/deploy-to-kubernetes.md)
- To keep the history you already have in weewx: [Bring an existing weewx
  database](docs/how-to/bring-an-existing-weewx-database.md)
- To try it without a station: [Run it locally](docs/how-to/run-it-locally.md),
  which includes a simulator that broadcasts made-up weather

Everything else is in [the documentation](docs/index.md).

## Why the pod is on the host network

Broadcasts reach only the network they were sent on, and a pod network is not
it. Both containers therefore share the node's network namespace, which makes
the dashboard's port reachable from your whole LAN, around the reverse proxy.
`WX_ALLOW_FROM` closes that gap. [The design notes](docs/explanation/design.md)
explain this and the other decisions.

## License

Apache 2.0; see [LICENSE](LICENSE). The weewx image installs weewx and
downloads the WeatherFlow UDP driver at build time; both are GPLv3 and are not
part of this repository.
