# Bring an existing weewx database

If weewx already records this station somewhere else, keep its history: copy
its database into the volume before the pod first starts, then repair two
things weewx installs commonly get wrong.

## 1. Check the unit system

weewx refuses to open a database written in a different unit system from the
one it is configured for. Find the database's:

```sh
sqlite3 weewx.sdb 'SELECT DISTINCT usUnits FROM archive'
```

`1` is US, `16` METRIC, `17` METRICWX. Set `target_unit` under `[StdConvert]`
in your `weewx.conf` to match. The dashboard reads any of the three.

## 2. Copy it into the volume

Copy with SQLite's backup API, which is safe while the old weewx is still
writing; `cp` is not. Apply your overlay with the Deployment scaled to zero
first, so the volume exists but nothing has it open:

```sh
kubectl apply -k my-weather/
kubectl -n weather scale deploy/stormkeep --replicas=0
```

Then run a one-off pod that mounts the volume and the old database, and runs
the backup. On the node where the old weewx runs, with its database in
`/var/lib/weewx`, that is:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: weewx-import
  namespace: weather
spec:
  restartPolicy: Never
  nodeName: the-node-with-the-old-database
  securityContext: {runAsUser: 10001, runAsGroup: 10001, fsGroup: 10001}
  containers:
    - name: import
      image: ghcr.io/runlevel-six/stormkeep-weewx:v0.1.0
      command: ["python3", "-c"]
      args:
        - |
          import sqlite3
          src = sqlite3.connect("file:/old/weewx.sdb?mode=ro", uri=True)
          dst = sqlite3.connect("/data/weewx.sdb")
          src.backup(dst)
          print(dst.execute("PRAGMA integrity_check").fetchone())
      volumeMounts:
        - {name: old, mountPath: /old, readOnly: true}
        - {name: data, mountPath: /data}
  volumes:
    - name: old
      hostPath: {path: /var/lib/weewx, type: Directory}
    - name: data
      persistentVolumeClaim: {claimName: weewx-data}
```

It should print `('ok',)`.

## 3. Rebuild the day summaries in your time zone

weewx keeps a summary row per day, and a day starts at local midnight **in the
time zone weewx ran in**. A weewx on a server set to UTC has been starting
each day at UTC midnight — the evening, in the Americas — and that is baked
into the summary table. With `TZ` now set to the station's zone, rebuild the
summaries. `rebuild-daily` alone does not do it: it keeps the existing rows
and adds new ones beside them. Drop them first.

Add a second container to the same pod spec, or run another one-off pod with
the volume and your `weewx-config` ConfigMap mounted at `/etc/weewx`, with
`TZ` set, and run:

```sh
weectl database drop-daily    --config=/etc/weewx/weewx.conf -y
weectl database rebuild-daily --config=/etc/weewx/weewx.conf -y
```

## 4. Calculate what was never calculated

weewx 5 calculates derived values (sea-level pressure, dew point, rain rate,
heat index, wind chill) only when `weewx.conf` has a `[StdWXCalculate]`
section listing them. A configuration without one stores NULL for all of them,
and CWOP then receives no pressure. The one in this repository has the
section; fill in the history with:

```sh
weectl database calc-missing --config=/etc/weewx/weewx.conf -y
```

This rewrites every record and took about 90 seconds for 150,000 records (a
year and a half at five minutes). The dashboard calculates these itself and
does not depend on this step; weewx's own reports and anything else reading
the database do.

## 5. Start, and run side by side

```sh
kubectl -n weather delete pod weewx-import
kubectl -n weather scale deploy/stormkeep --replicas=1
```

Leave CWOP disabled. Both weewx installations now record the same
broadcasts — the driver's `share_socket` lets them share the port — so you can
compare them for as long as you like. The history between the copy and the
start is a gap of a few minutes; software record generation cannot backfill
it.

## 6. Retire the old weewx

Stop and disable the old one, then [turn on CWOP](deploy-to-kubernetes.md#5-turn-on-cwop-if-you-report-there)
in the new one. Keep the old database file until you are satisfied; nothing
here deletes it.
