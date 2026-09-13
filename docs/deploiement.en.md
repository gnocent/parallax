# Parallax — deployment

*[Français](deploiement.md) · English*

This document covers deployment to an internal RHEL 8 or 9 server.
It complements the principles in CLAUDE.md without
repeating them.

---

## 1. Compile

From a machine with the Go toolchain (WSL2 or Linux), at the root of the repository:

```sh
make release
```

Produces `bin/parallax`, a **statically linked** Linux amd64 executable —
`CGO_ENABLED=0` and the pure-Go SQLite driver (`modernc.org/sqlite`)
guarantee this. Verifiable:

```sh
file bin/parallax   # « statically linked »
ldd bin/parallax     # « not a dynamic executable »
```

No dependency on the glibc version of the build machine: the
binary runs as-is on RHEL 8 or 9, with nothing else to install besides
transferring it.

## 2. Transfer and first launch

```sh
scp bin/parallax serveur-cible:/opt/parallax/parallax
ssh serveur-cible
sudo mkdir -p /var/lib/parallax
sudo /opt/parallax/parallax -base /var/lib/parallax/parallax.db -adresse :8080
```

On the very first launch against a fresh database, an administrator account is
created automatically and its password **printed only once** in the
logs:

```
aucun compte administrateur trouvé : compte créé — login « admin », mot de passe « ... »
```

Note it down immediately (it cannot be displayed again) and change it at the
first login (Accounts screen → Password). Stop the manual
launch (Ctrl+C — the shutdown is clean, see §5) before moving on to the
systemd unit.

## 3. Configuration

Two equivalent ways, the second useful for a systemd unit:

| Flag | Environment variable | Default | Role |
|---|---|---|---|
| `-base` | `PARALLAX_BASE` | `parallax.db` | path to the SQLite file |
| `-adresse` | `PARALLAX_ADRESSE` | `:8080` | HTTP listening address |

An explicit flag on the command line takes precedence over the environment
variable.

No built-in TLS: the binary serves plain HTTP. On a
segregated network, a reverse proxy (nginx, haproxy) terminates TLS and
forwards over internal HTTP — configuration outside the scope of this document,
the responsibility of the infrastructure team that already manages these reverse proxies.

## 4. systemd unit

`/etc/systemd/system/parallax.service`:

```ini
[Unit]
Description=Parallax — inventaire matériel et capacity planning
After=network.target

[Service]
Type=simple
User=parallax
Group=parallax
Environment=PARALLAX_BASE=/var/lib/parallax/parallax.db
Environment=PARALLAX_ADRESSE=127.0.0.1:8080
ExecStart=/opt/parallax/parallax
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

`TimeoutStopSec` slightly above the 10 seconds the binary
gives itself to finish in-flight requests (§5): systemd must
let the clean shutdown run its course before cutting it off with the KILL signal.

Create the system user and permissions, then enable:

```sh
sudo useradd --system --home /var/lib/parallax --shell /usr/sbin/nologin parallax
sudo mkdir -p /var/lib/parallax && sudo chown parallax:parallax /var/lib/parallax
sudo systemctl daemon-reload
sudo systemctl enable --now parallax
sudo systemctl status parallax
```

## 5. Clean shutdown and supervision

The binary intercepts SIGTERM (what `systemctl stop` sends) and SIGINT
(Ctrl+C): it stops accepting new requests, allows up to 10
seconds for in-flight requests to complete, then shuts down. A
restart or a deployment never cuts off a write in progress.

`GET /sante` responds `200 ok` without authentication if the database responds, `503`
otherwise — to be probed by the reverse proxy or a systemd `ExecStartPost`.

## 6. Backup

### 6.1 On-demand local copy

`-sauvegarde` produces a consistent copy without interrupting the service
(`VACUUM INTO`, two separate processes can open the same
SQLite file in WAL mode):

```sh
/opt/parallax/parallax -base /var/lib/parallax/parallax.db \
  -sauvegarde /var/backups/parallax/parallax-$(date +%F).db
```

Usable via cron if you don't want the built-in S3 deposit (§6.2):

```cron
0 2 * * * parallax /opt/parallax/parallax -base /var/lib/parallax/parallax.db -sauvegarde /var/backups/parallax/parallax-$(date +\%F).db
```

### 6.1 bis Automatic local backup (v3.7)

Built into the binary, configured from the **Backup** screen
(`/parametres/sauvegarde`, administrator) rather than an environment
variable: destination folder, daily time, and retention in days. As long
as no folder or no time is set, it stays disabled — no default chosen in
place of the administrator.

Every day at the configured time, a new copy is written only if the
change log shows activity since the previous successful backup
(`depot.ActiviteDepuis`) — a quiet night produces no redundant file.
Copies older than the configured retention are deleted automatically,
regardless of that day's outcome. A **Back up now** button writes a copy
immediately, handy for checking that the chosen folder is actually
writable without waiting for the scheduled time.

Independent from the periodic S3 deposit (§6.2): both can run at the same
time, each on its own schedule.

### 6.2 Periodic deposit to internal S3 object storage

Built into the binary (package `internal/sauvegarde`, SigV4 client without an SDK).
Target: a **local** S3-compatible object store (MinIO or equivalent),
over HTTPS with the internal certificate authority. The service starts even
if the store is unreachable (per the "no network dependency at
startup" principle): the failure is logged and retried on the next cycle.

| Variable | Default | Role |
|---|---|---|
| `PARALLAX_S3_ENDPOINT` | — | full URL, scheme required: `https://objets.interne:9000` |
| `PARALLAX_S3_BUCKET` | — | destination bucket |
| `PARALLAX_S3_PREFIXE` | `parallax/` | key prefix |
| `PARALLAX_S3_REGION` | `us-east-1` | signed region (value expected by most local S3 stores) |
| `PARALLAX_S3_CLE_ACCES` | — | access key id |
| `PARALLAX_S3_CLE_SECRETE` | — | secret key |
| `PARALLAX_S3_CA` | — | PEM file for the internal certificate authority (otherwise, the system store) |
| `PARALLAX_S3_STYLE_VIRTUEL` | `0` | `1` for `bucket.endpoint` addressing; defaults to `endpoint/bucket` (MinIO) |
| `PARALLAX_SAUVEGARDE_INTERVALLE` | `24h` | deposit period (`0` disables) |

The deposit is active as soon as the endpoint, bucket, and both keys are
provided. Each cycle: `VACUUM INTO` a temporary file, upload under
the key `<préfixe>parallax-AAAAMMJJ-HHMMSS.db`, deletion of the temporary file.
No purging on the application side: retention is set on the bucket
(lifecycle policy), where it is audited.

`-sauvegarde-s3` runs one deposit immediately then exits — to validate
the configuration before letting the service run continuously:

```sh
sudo -u parallax /opt/parallax/parallax -sauvegarde-s3
```

**The keys never go into the database** (the database is precisely what is
being backed up, and any reader of the database would read the keys to its own
backup store). They live in an environment file readable by the
service alone:

```sh
sudo install -m 0600 -o root -g parallax /dev/null /etc/parallax/secrets
sudo tee /etc/parallax/secrets >/dev/null <<'EOF'
PARALLAX_S3_ENDPOINT=https://objets.interne:9000
PARALLAX_S3_BUCKET=sauvegardes-parallax
PARALLAX_S3_CA=/etc/parallax/ca-interne.pem
PARALLAX_S3_CLE_ACCES=…
PARALLAX_S3_CLE_SECRETE=…
EOF
```

and in the systemd unit (§4), under `[Service]`:

```ini
EnvironmentFile=/etc/parallax/secrets
```

### 6.2 bis Behind a TLS reverse proxy

When a proxy terminates TLS and talks to Parallax over plain HTTP, set
`PARALLAX_COOKIES_SECURE=1` in the environment file: the session cookie
then carries the Secure attribute even though the request it receives is not
encrypted. Without a proxy, over internal HTTP, leave it unset.

### 6.3 LDAP authentication (v3.6)

Optional: without `PARALLAX_LDAP_URL`, only local accounts exist.
Simple LDAPv3 bind over TLS, using the user's own identity — no
service account, and therefore no secret to add to the environment file.

| Variable | Role | Example |
|---|---|---|
| `PARALLAX_LDAP_URL` | directory, must be `ldaps://` | `ldaps://annuaire.exemple.org:636` |
| `PARALLAX_LDAP_DN` | bind DN template, `{login}` replaced by the entered identifier | `uid={login},ou=people,dc=exemple,dc=org` |
| `PARALLAX_LDAP_CA` | internal certificate authority, PEM file readable by the service | `/etc/parallax/ca-interne.pem` |
| `PARALLAX_LDAP_DELAI` | connection and response timeout, default `5s` | `5s` |

An account created by an LDAP login is born as READER; an administrator
promotes it. Keep at least one local administrator: if the directory is down or
misconfigured, it remains the only way in.

## 7. Go-live checklist

- [ ] Administrator account created, password changed, accounts for the 3-4
      editors created with the right role
- [ ] Import of reference data, the hardware catalog, and the real inventory
      (`/import` screen, or manual entry if the volume allows)
- [ ] Formula acceptance testing against the current Excel sheet on at least three
      clusters of different technologies — see the "After the import" section of
      [`import-format.en.md`](import-format.en.md#after-the-import)
- [ ] Automated backup verified with a test restore
- [ ] Editor training on the view builder (it is what
      replaces pivot tables day to day)
- [ ] Actual go-live, old Excel workbook frozen read-only
