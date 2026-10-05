# Cut a release

A release is a signed `v` tag on master. CI does the rest.

```sh
git tag -s v0.2.0 -m "v0.2.0"
git push origin master
git push origin v0.2.0
```

Push the branch and the tag separately: pushed together, the tag push has been
seen to start no workflow run. If that happens, run the CI workflow by hand
from the tag (Actions → CI → Run workflow → Tags).

On a tag, CI builds and tests, then publishes both images:

- `ghcr.io/runlevel-six/stormkeep:v0.2.0`
- `ghcr.io/runlevel-six/stormkeep-weewx:v0.2.0`

It refuses to publish a version that already exists, and checks the dashboard
image reports the version it was published under. Every push to master also
publishes `edge`, and every build a `sha-` tag.

## Versions

A minor version for anything a viewer or an operator would notice; a patch
version for fixes. Deployments pin a version, and the overlay's `ref=` and
image tags should name the same one.
