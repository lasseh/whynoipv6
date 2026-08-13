# Runbook — Cloudflare Origin CA certificate

`whynoipv6.com`, `www` and `api` are all Cloudflare-proxied, and the zone
runs Automatic SSL/TLS currently resolved to **Full (strict)**. Cloudflare
never downgrades that: an expired origin cert does not quietly drop the zone
to Full, it makes the edge return **526 to every visitor**. A 90-day Let's
Encrypt cert therefore makes each renewal an availability dependency.

Full (strict) accepts a cert "issued by a publicly trusted certificate
authority **or Cloudflare's Origin CA**", and an Origin CA cert lasts up to
15 years. One apex + wildcard cert covers all three server blocks and takes
ACME off this host entirely (09-ops.md §7.1).

The Ansible side is already built (`roles/whynoipv6/tasks/origin_cert.yml`).
Everything below is the part only you can do.

## 1. Create the certificate

Cloudflare dashboard → **SSL/TLS** → **Origin Server** → **Origin
Certificates** tab → **Create Certificate**.

| Field | Choose | Why |
|---|---|---|
| Private key type | **ECC** | Only the Cloudflare edge connects here, and it does ECDSA; smaller and faster than RSA. |
| Hostnames | `whynoipv6.com` **and** `*.whynoipv6.com` | Both are pre-filled — **keep both.** The field is editable, and an apex-only cert fails in the least obvious way: the site works while every `api.*` request becomes a 526. |
| Certificate Validity | **15 years** | The whole point. |
| Key format | **PEM** | The OpenSSL/nginx format. |

**The private key is shown exactly once.** Cloudflare's wording: *"For
security reasons, you cannot see the Private Key after you exit this
screen."* Copy both blobs somewhere before you navigate away.

## 2. Store it in the vault

```bash
cd ~/code/go/src/github.com/lasseh/ansible
ansible-vault edit inventory/vault_vars/web02.lasse.cloud.vault.yml
```

PEM is multi-line, so use a YAML literal block scalar (`|`) and indent the
body. Do **not** quote it or fold it with `>`:

```yaml
whynoipv6_origin_cert: |
  -----BEGIN CERTIFICATE-----
  MIIExxxx...
  -----END CERTIFICATE-----

whynoipv6_origin_cert_key: |
  -----BEGIN PRIVATE KEY-----
  MIIEvxxxx...
  -----END PRIVATE KEY-----
```

## 3. Flip the host over

In `inventory/host_vars/web02.lasse.cloud.yml`:

```yaml
whynoipv6_use_origin_cert: true
whynoipv6_obtain_certificate: false   # no ACME challenge to serve any more
```

The second line matters: without it the role still runs a certbot round for
names that no longer need one, and Let's Encrypt rate-limits failed
validations.

## 4. Deploy

```bash
ansible-playbook playbooks/web02.lasse.cloud.yml
```

Before anything lands on the live paths, the role stages the pair and
checks that the key matches the certificate, that it is not expired, and
that its SANs cover every configured hostname. Any of those failing aborts
the play rather than reloading nginx into a site-wide 526. It prints the
issuer, subject and expiry of the cert it is about to install — read that
line.

## 5. Verify

On the host — expect a Cloudflare Origin CA issuer and a date ~15 years out:

```bash
echo | openssl s_client -connect 127.0.0.1:443 -servername whynoipv6.com 2>/dev/null \
  | openssl x509 -noout -issuer -subject -dates
```

Then through the edge. All three must be 200; a **526 means Full (strict)
rejected the origin cert**:

```bash
for h in whynoipv6.com www.whynoipv6.com api.whynoipv6.com; do
  printf '%-24s %s\n' "$h" "$(curl -sS -o /dev/null -w '%{http_code}' "https://$h/")"
done
```

## 6. Retire certbot for these names

Only once step 5 is green, so the old certs stay available as a fallback
until then:

```bash
sudo certbot certificates                        # confirm what is there
sudo certbot delete --cert-name whynoipv6.com
sudo certbot delete --cert-name api.whynoipv6.com
```

Leave certbot itself installed and running — anything on this host that is
*not* behind Cloudflare still needs it.

## Rollback

Set `whynoipv6_use_origin_cert: false` and
`whynoipv6_obtain_certificate: true`, re-run the playbook. The cert paths
revert to `/etc/letsencrypt/live/...`, which is why the Origin CA pair lives
in `/etc/ssl/cloudflare/` instead — the two trees never contend, so this is
a variable flip rather than a file rescue. If you already did step 6,
certbot re-issues on that run.

## Constraints to remember

- **Not browser-trusted.** Only the Cloudflare edge accepts this cert.
  Pausing the zone, grey-clouding a record, direct-IP testing, or pointing
  an uptime monitor at the origin instead of the edge will all fail
  validation. Any hostname that leaves the proxy must go back to certbot
  *first*.
- **Self-signed and private-CA certs are not substitutes.** Full (strict)
  rejects both — that path silently costs you origin validation.
- **A wildcard covers one level.** `*.whynoipv6.com` matches
  `api.whynoipv6.com` but not `a.b.whynoipv6.com`.
- **The Cloudflare root** is only needed if you later enable Authenticated
  Origin Pulls, where nginx verifies the edge's *client* cert
  (`ssl_client_certificate`). Serving under Full (strict) needs just the
  cert and key.
- **Expiry monitoring is not optional.** 15 years removes the renewal
  treadmill and replaces it with a very long blind spot. This design defers
  that failure rather than removing it.

## Rotation

There is no renewal, but you may need to replace the cert (key exposure, or
an added hostname outside the wildcard). It is the same procedure: create a
new one, update the vault, re-run. Revoke the old one afterwards in the same
dashboard tab.
