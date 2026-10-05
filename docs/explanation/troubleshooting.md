# Troubleshooting

## The page never says "Live"

The dashboard has received no broadcast. Its log says `no broadcasts from the
station` after three minutes of silence.

- Is weewx receiving them? `kubectl logs ... -c weewx` shows `Added record`
  every five minutes when it is. If neither container hears anything, the
  broadcasts are not reaching the node: check the pod is on a node on the
  station's network, and that `hostNetwork: true` survived your overlay.
- Is the hub broadcasting? The Tempest app's hub settings can turn local UDP
  off.
- Is a firewall on the node dropping UDP 50222?
- If only the dashboard is silent, check `WX_STATION_SERIAL` matches the
  sensor exactly.

## Every page answers 403

`WX_ALLOW_FROM` does not include the address the ingress controller connects
from. The dashboard logs one `refused a request` line a minute naming that
address; add its network. Behind some network setups the ingress reaches a
host-network pod from the node's address rather than its own; if the logged
address is the node's, allow that address alone, not the LAN it is on.

## Today's high, low or rain is empty, or resets in the evening

`TZ` differs between the two containers, or weewx ran in another zone when
the day summaries were written. Set the same `TZ` for both, and rebuild the
summaries as in [Bring an existing weewx
database](../how-to/bring-an-existing-weewx-database.md#3-rebuild-the-day-summaries-in-your-time-zone).

## "The weather archive can't be read right now"

The dashboard cannot open `WX_DB`. On a fresh install weewx creates it on its
first start; otherwise check the volume is mounted in both containers and
that weewx is running. Current conditions keep working meanwhile.

## weewx exits with a unit system mismatch

`target_unit` under `[StdConvert]` differs from the database's. Set it to the
database's own; see [Configuration](../reference/configuration.md#weewx).

## A record looks impossible

The dashboard shows the archive as it is. A sensor glitch that weewx stored —
a burst of impossible wind, say — will hold a record until it is removed from
the database. `[StdQC]` stops new ones; existing ones need deleting by hand
followed by `weectl database rebuild-daily`.
