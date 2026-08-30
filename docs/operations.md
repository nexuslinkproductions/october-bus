# Local runtime operations

October Bus runs as one local daemon. The reference profile binds to `127.0.0.1` and stores accepted work in SQLite.

## Start and stop

```bash
october-bus start
october-bus status
october-bus doctor
october-bus stop
```

`stop` sends an authenticated shutdown request through the local endpoint. The admin token stays in the protected run file and is not passed on the command line.

Use `october-bus doctor --json` for machine-readable diagnostics. It reports versions, paths, process state, and endpoint health. It does not print credentials or message content.

## Default paths

| Platform | Data directory | Runtime directory |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\October Bus` | Per-user temporary directory |
| macOS | `~/.local/share/october-bus` | Per-user temporary directory |
| Linux | `$XDG_DATA_HOME/october-bus` or `~/.local/share/october-bus` | `$XDG_RUNTIME_DIR/october-bus` or a per-user temporary directory |

Set `OCTOBER_BUS_DATA_DIR` and `OCTOBER_BUS_RUNTIME_DIR` to override these paths. The data directory contains `bus.db`. The runtime directory contains the current lock and credential-bearing run file.

Do not share the runtime directory. Back up the data directory only while the daemon is stopped until online backup support is documented.

## Supervision

The daemon handles interrupt and termination signals and shuts down active HTTP requests before closing SQLite. A service supervisor should restart unexpected exits with bounded backoff.

The repository does not yet ship platform service definitions. Keep credentials out of service arguments and logs when adding one.

## Recovery

Accepted messages and active tasks survive a normal daemon restart. If startup reports an invalid database schema or integrity problem, preserve the database before attempting repair. Automated migration and repair tools are planned before the first stable release.
