# Run it locally

The dashboard needs two things: broadcasts to listen to, and a weewx database
to chart. Locally the first can come from the simulator; the second is
optional.

You need Go (the version in `go.mod`) and `make`.

## With simulated weather only

```sh
make run
```

This builds both programs, starts `tempest-sim` sending made-up broadcasts to
`127.0.0.1:50222`, and serves the dashboard at <http://127.0.0.1:8080>. The
current conditions fill in within a minute and the wind moves every three
seconds. The history charts stay empty, since there is no database; the page
says so rather than failing.

To make the simulated day pass faster, run the pieces yourself:

```sh
make build
bin/tempest-sim -to 127.0.0.1:50222 -speed 30 &
WX_LISTEN=127.0.0.1:8080 bin/stormkeep
```

## With real history

Copy a weewx database somewhere outside the repository's tracked files (any
`*.sdb` is ignored by git) and point `WX_DB` at it:

```sh
WX_DB=/path/to/weewx.sdb make run
```

Copy the file with SQLite's backup API, not `cp`, if weewx is writing to it:

```sh
sqlite3 /var/lib/weewx/weewx.sdb ".backup '/tmp/weewx.sdb'"
```

Set `TZ` to the time zone the database's weewx runs in, or "today" will be the
wrong day: see [Configuration](../reference/configuration.md#tz).

## Before sending a change

```sh
make check   # gofmt, vet, the tests with -race, and a JavaScript syntax check
```

CI also runs golangci-lint (the version is pinned in
`.github/workflows/ci.yaml`) and misspell with the US locale.
