# HTTP API

The page is built on three endpoints, which are also usable directly.

Every value is in **°C, m/s, hPa, mm, mm/h and km**, whatever unit system
the weewx database uses; the page converts for display. A missing value is
`null`. Times are Unix seconds, or `null`.

## `GET /api/now`

Current conditions and the day's figures. Cached for five seconds.

| Field | Meaning |
| --- | --- |
| `time` | When the current conditions were measured. |
| `source` | `live` when they came from a broadcast, `archive` when no broadcast has arrived since the dashboard started and they are the newest archive record. |
| `current` | `temp`, `feels`, `dew`, `humidity`, `pressure` (sea level), `pressureTrend` (change over three hours), `stationPressure`, `wind`, `gust`, `lull`, `windDir`, `rainRate`, `uv`, `solar`, `illuminance`, `precipType` (0 none, 1 rain, 2 hail, 3 both). |
| `windNow`, `windRecent` | The latest three-second wind sample and the last ten minutes of them, each `{t, s, d}`: time, speed, direction. |
| `today` | `high`, `low`, `gust` as `{v, t}`, and `strikes`. A live reading past the archive's extremes counts. |
| `rain` | `hour`, `day` (last 24 hours), `today`, `yesterday`, `month`, `year`, and `started`, the station's last "rain has begun". |
| `lightning` | `count3h` and `last` (`{t, km}`), from broadcasts since the dashboard started. |
| `records` | `since` (the first archive record), and `high`, `low`, `gust`, `wettest` as `{v, t}`. |
| `health` | `lastPacket`, `battery` (volts), `rssi` and `hubRssi` (dBm), and `archive`, the newest archive record. |
| `errors` | Present when part of the response could not be read, naming which part: `archive`, `today`, `records`, and so on. The rest is still filled in. |

## `GET /api/history?range=day|week|month|year`

The archive summarized into points for a chart. Cached for a minute.

| Range | Span | Each point |
| --- | --- | --- |
| `day` | 24 hours | 10 minutes |
| `week` | 7 days | 1 hour |
| `month` | 30 days | 6 hours |
| `year` | 365 days | 1 calendar day |

The response holds `from`, `to`, `width` (seconds per point), and one array
per measure, all the same length: `t` (each point's start), `temp`, `dew`,
`humidity`, `pressure`, `wind` (means); `gust`, `uv` (highest); `rain`,
`strikes` (totals); `solar` (mean); `windDir` (mean direction, weighted by
speed). A point with no records is all `null`, so a chart shows the gap.

## `GET /api/stream`

[Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html)
as broadcasts arrive:

| Event | Data |
| --- | --- |
| `wind` | `{t, s, d}`, every three seconds |
| `obs` | `{t}`: a full observation has arrived; fetch `/api/now` |
| `strike` | `{t, km}` |
| `rain` | `{t}`: rain has begun |

A comment line every 20 seconds keeps idle proxies from closing the
connection.

## `GET /healthz`

`ok`, always, from anywhere: it is exempt from `WX_ALLOW_FROM` because the
kubelet probes from the node.
