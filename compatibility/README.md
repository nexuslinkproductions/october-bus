# Compatibility evidence

October Bus names a harness as verified only when a current public evidence record passes the applicable conformance profile.

The registry starts empty. Experimental adapter manifests do not count as compatibility evidence.

Each evidence record must validate against [`compatibility-evidence.schema.json`](../spec/0.1/schemas/compatibility-evidence.schema.json) and include the harness version, adapter version, Bus versions, platform, result digest, verification time, repository commit, limitations, and verification mode. The registry itself is validated against [`compatibility-registry.schema.json`](../spec/0.1/schemas/compatibility-registry.schema.json).

`registry.json` contains paths to current passing evidence. Failed or stale records may remain for history but must be removed from the registry.
