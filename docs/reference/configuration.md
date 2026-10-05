# Configuration

## The dashboard

All configuration is by environment variable. In the published manifests both
containers read the ConfigMap `stormkeep-env`; weewx ignores the
`WX_` variables.

| Variable | Default | Meaning |
| --- | --- | --- |
| `WX_LISTEN` | `:8080` | Address the HTTP server binds. The manifests set `$(HOST_IP):8420`: on the host network, bind the node's own address and a port nothing else uses. |
| `WX_DB` | `/data/weewx.sdb` | The weewx database, opened read-only. It may not exist yet; the live half of the page works without it. |
| `WX_UDP` | `:50222` | Where to listen for the station's broadcasts. Empty turns listening off. |
| `WX_STATION_SERIAL` | (any) | Accept messages from this sensor only, such as `ST-00000000`. Matters only if a second station broadcasts on the same network. |
| `WX_ALTITUDE` | `0` | The station's altitude in meters, for sea-level pressure. Use the same value as `weewx.conf`. At `0` the station pressure is shown unchanged. |
| `WX_NAME` | `Weather` | The page title. |
| `WX_UNITS` | `us` | Units a first-time viewer sees: `us` (°F, mph, inHg, in) or `metric` (°C, km/h, hPa, mm). Each viewer can switch; the choice is kept in their browser. |
| `WX_ALLOW_FROM` | (everyone) | Comma-separated networks, such as `10.244.0.0/16`, allowed to load anything but `/healthz`. Everyone else gets 403. See [Design](../explanation/design.md#what-guards-the-port). |
| `WX_ALLOW_INDEXING` | `false` | `true` drops the `noindex` header, for a public page that should appear in search results. |
| `TZ` | `UTC` | See below. |

### TZ

The time zone that "today", "yesterday", "this month" and "this year" are
counted in. It **must be the same zone weewx runs in**: weewx stores one
summary row per day starting at its local midnight, and the dashboard looks
the rows up by its own. If they differ, today's figures come back empty or
from the wrong day. The image has the zone database built in; use a name like
`America/Denver`, not an abbreviation.

## weewx

`deploy/weewx.conf` is a complete configuration. These are the parts that are
yours:

| Where | What |
| --- | --- |
| `[Station]` `location`, `latitude`, `longitude` | Shown to CWOP; position is used for solar calculations. |
| `[Station]` `altitude` | Turns station pressure into sea-level pressure. |
| `[WeatherFlowUDP]` `[[sensor_map]]` | Replace `ST-00000000` with your sensor's serial number, on every line. |
| `[StdRESTful]` `[[CWOP]]` | `station` is your CWOP ID; `enable` stays `false` until this is the only weewx for the station. |
| `[StdConvert]` `target_unit` | Must match an existing database's unit system. `METRICWX` for a new one. |
| `[StdQC]` `[[MinMax]]` | Readings outside these bounds are dropped as sensor errors. |

Do not remove `[StdWXCalculate]`: without it weewx 5 calculates no derived
values at all. The `[Logging]` section sends logs to standard output, where
`kubectl logs` finds them.

## Kubernetes objects

| Object | Name | Notes |
| --- | --- | --- |
| Deployment | `stormkeep` | One replica, `Recreate`, host network. |
| Service | `stormkeep` | Port 80 to the dashboard's `http` port. |
| Ingress | `stormkeep` | Host is a placeholder; add your class, TLS and authentication. |
| PersistentVolumeClaim | `weewx-data` | 1 Gi, `ReadWriteOnce`. weewx mounts it read-write, the dashboard read-only. |
| ConfigMap | `weewx-config` | `weewx.conf`, mounted at `/etc/weewx`. |
| ConfigMap | `stormkeep-env` | Environment for both containers. |

Both images run as UID and GID 10001 with a read-only root file system.
