# Design

## Why the pod is on the host network

The Tempest hub sends its readings as UDP broadcasts to port 50222. A
broadcast is delivered to every machine on the network segment it was sent on
and goes no further: routers do not forward it, and neither does the virtual
network a Kubernetes pod sits on. A pod on the pod network will never receive
a single packet, however its Service is set up — which is the usual reason an
attempt to run weewx's WeatherFlow driver in Kubernetes fails silently.

So the pod uses `hostNetwork: true`, putting both containers in the node's own
network namespace, where the broadcasts arrive. The cost is that the pod
pins to a node on the station's network, and that its ports are the node's
ports.

The alternative — a small relay on the host network forwarding each datagram
to a pod-network Service — adds a moving part and lets anything that can
reach the Service inject weather. It was not worth it for one station.

## What guards the port

On the host network the dashboard's listener is reachable from every machine
on the LAN, not only through the ingress controller, so whatever
authentication the ingress adds can be walked around. A NetworkPolicy cannot
help: policies do not apply to host-network pods.

Two things close that gap. The dashboard binds the node's address on a port
of its own (8420), not every interface. And `WX_ALLOW_FROM` makes it refuse
any request whose TCP peer is outside the listed networks — the ingress
controller's pod range — with a 403. It checks the connection's source
address, never `X-Forwarded-For`, which a client can set to anything.
`/healthz` is exempt because the kubelet probes from the node itself, and it
reveals nothing.

## Why weewx stays

The dashboard could decode the broadcasts, keep its own history and post to
CWOP itself, and the whole thing would be one binary. weewx is kept because it
already does the parts that are easy to get subtly wrong: quality control,
the derived quantities and their standard formulas, the per-day summaries,
the CWOP packet format and its rules about timing, and a database format
other weewx tools understand. It also means an existing weewx history comes
along unchanged.

The split is strict: weewx owns the database and is the only writer; the
dashboard opens it read-only and waits out weewx's locks. Each listens to the
broadcasts for itself — the driver's `share_socket` and the dashboard's
listener both set `SO_REUSEADDR`, and the kernel hands every broadcast to both
— so the live page does not wait for weewx's five-minute records.

## The driver patch

The WeatherFlow UDP driver turns every datagram into Python source and passes
it to `eval()`. Anything that can send one UDP packet to the station's port —
any device on the network — can run code inside weewx. The image patches that
to `json.loads()`, and moves the driver's logging from syslog, which a
container does not have, to Python's logging. The patch is exact-text edits
against a pinned commit and fails the build if upstream changes.

## Derived values are calculated twice, on purpose

The dashboard calculates dew point, feels-like and sea-level pressure from the
raw measurements, for live readings and history alike, rather than reading
weewx's columns. Those columns are empty in any database written by a weewx 5
configuration without `[StdWXCalculate]`, and the live readings never pass
through weewx at all. Calculating both the same way means a chart never steps
where history hands over to the live reading. weewx still calculates its own
for CWOP and for anything else that reads the database.

## Time zones

weewx keeps a summary row per day, beginning at local midnight in the zone it
runs in, and the dashboard reads those rows for today's high, low and rain. A
container defaults to UTC, which would start the day in the evening across
the Americas. Both containers read the same `TZ`, and the weewx image fails to
build if its zone database is missing, because glibc would otherwise fall back
to UTC without saying so.

## Units

The API speaks one set of units (°C, m/s, hPa, mm), whatever weewx stores;
the page converts. A viewer's choice of US or metric lives in their browser,
so two people can read the same dashboard in different units, and changing it
costs no request.

## The page

One HTML page, one script and one stylesheet, with no framework and nothing
loaded from elsewhere, served under a content security policy that allows
only the page's own origin. The charts are drawn as SVG by hand: a crosshair
shared across all of them, one measure per chart rather than two axes on one,
and a table view of the same numbers for anyone who cannot use the charts.
The series colors come from a palette checked for color-vision deficiency,
and text never takes a series color.
