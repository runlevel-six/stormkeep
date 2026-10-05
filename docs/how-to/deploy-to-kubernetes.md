# Deploy to Kubernetes

The published manifests in `deploy/` are a base with placeholders. You deploy
an overlay that fills them in.

## Before you start

You need:

- A node on the same network as the Tempest hub, so its broadcasts reach it.
  The pod runs on the host network (see [Design](../explanation/design.md)), so
  it must be able to run there: `hostNetwork` must be allowed by any pod
  security policy you enforce.
- A free TCP port on that node. The base uses **8420**; change it in both
  `WX_LISTEN` and the container port if something else has it.
- A storage class that gives a block-backed `ReadWriteOnce` volume. SQLite
  locking is unreliable on network file systems.
- An ingress controller, and the address range its pods connect from, for
  `WX_ALLOW_FROM`.
- Your station's serial number (`ST-` followed by eight digits, on the
  sensor, or in any `obs_st` broadcast), its latitude, longitude and altitude.

## 1. Write the overlay

Keep it out of this repository (the `.gitignore` reserves `deploy-local/` if
you want it beside the base). For example:

```text
my-weather/
├── kustomization.yaml
└── weewx.conf
```

Copy `deploy/weewx.conf` to `weewx.conf` and replace every `REPLACE` and every
`ST-00000000`, plus the latitude, longitude and altitude. Leave CWOP disabled
for now.

`kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: weather

resources:
  - namespace.yaml   # or create the namespace yourself
  - https://github.com/runlevel-six/stormkeep//deploy?ref=v0.1.0

images:
  - name: ghcr.io/runlevel-six/stormkeep
    newTag: v0.1.0
  - name: ghcr.io/runlevel-six/stormkeep-weewx
    newTag: v0.1.0

configMapGenerator:
  - name: weewx-config
    behavior: replace
    files:
      - weewx.conf
  - name: stormkeep-env
    behavior: merge
    literals:
      - TZ=America/Denver             # the station's own time zone
      - WX_NAME=Home weather
      - WX_ALTITUDE=12                # meters, the same as weewx.conf
      - WX_STATION_SERIAL=ST-00000000
      - WX_ALLOW_FROM=10.244.0.0/16   # where your ingress controller's pods live

patches:
  - target: {kind: PersistentVolumeClaim, name: weewx-data}
    patch: |
      - op: add
        path: /spec/storageClassName
        value: my-block-storage
  - target: {kind: Ingress, name: stormkeep}
    patch: |
      - op: replace
        path: /spec/rules/0/host
        value: weather.example.com
      - op: add
        path: /spec/ingressClassName
        value: my-ingress-class
```

Pin `ref=` and the image tags to the same release, so the manifests and the
images always match. Add whatever your ingress needs for TLS and for
authentication; the dashboard has no login of its own.

## 2. Bring history, if you have it

If weewx already runs somewhere for this station, follow [Bring an existing
weewx database](bring-an-existing-weewx-database.md) now, before the first
start. Otherwise weewx creates an empty database on its first start.

## 3. Apply it

```sh
kubectl apply -k my-weather/
kubectl -n weather rollout status deploy/stormkeep
```

## 4. Check it

```sh
kubectl -n weather logs deploy/stormkeep -c weewx | grep -E 'Starting up|Added record'
kubectl -n weather logs deploy/stormkeep -c dashboard
```

Within five minutes weewx logs `Added record`. The dashboard logs `serving`
and, if `WX_ALLOW_FROM` is wrong, a `refused a request` line naming the
address the ingress really connects from.

Open the site: the header says **Live** once the first broadcast lands, and
the wind needle moves every few seconds.

## 5. Turn on CWOP, if you report there

Only when this is the one weewx running for the station: set
`enable = true` under `[[CWOP]]` in your `weewx.conf` and apply again. weewx
logs `CWOP: Published record` every five minutes.
