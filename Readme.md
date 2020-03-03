# vault-couchbase-database-plugin

Couchbase has no supported plugins for the Vault database secrets engine. This is a custom Vault plugin which is used for generating database credentials dynamically based on configured roles for the Couchbase.

## **Usage:**

- Build and copy the binary to the Vault custom plugins directory, which is configured by "plugin_directory" parameter on Vault. More information at [https://www.vaultproject.io/docs/configuration/index.html#inlinecode-plugin_directory](https://www.vaultproject.io/docs/configuration/index.html#inlinecode-plugin_directory)
- Add Couchbase plugin to vault;

```bash
vault write sys/plugins/catalog/database/couchbase-database-plugin \
    sha256=<SHA256 sum of plugin binary> \
    command="couchbase-database-plugin"
```

_Note: You can generate the sha256 sum of binary by executing 'sha256sum -b couchbase-database-plugin'_

- Enable the database secrets engine;

```bash
vault secrets enable -path=couchbase database
```

- Configure Vault with the couchbase plugin and the connection information;

_Note: bucket can be any bucket on your cluster. It is only needed because you cannot perform cluster level operations without opening a bucket on Couchbase servers version lesser than 6.5._

```bash
vault write couchbase/config/example-db \
    plugin_name=couchbase-database-plugin \
    allowed_roles="example-app" \
    connection_string="couchbase://<cb-node-ip-1>,<cb-node-ip-2>" \
    username="<couchbase-admin-username>" \
    password="<couchbase-admin-password>" \
    bucket="<bucket-name>"
```

- Configure a role that maps a name in Vault to a Couchbase command that executes and creates the database credential;
  _For more information about role based access control on Couchbase, [https://docs.couchbase.com/server/6.5/learn/security/roles.html](https://docs.couchbase.com/server/6.5/learn/security/roles.html)_

```bash
vault write couchbase/roles/example-app \
    db_name=example-db \
    creation_statements="{\"roles\": [{\"role\": \"bucket_full_access\",\"bucket_name\": \"Products\"}]}" \
    default_ttl="1h" \
    max_ttl="24h"
```

- Generate and read a new credential;

```bash
vault read couchbase/creds/example-app
```

---

For more information about vault custom database plugins;
https://www.vaultproject.io/docs/secrets/databases/custom/
https://www.vaultproject.io/docs/internals/plugins/
