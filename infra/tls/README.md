# TLS Certificate Provisioning Guide

This directory holds TLS certificates for securing internal communication
between Mint components in non-development deployments.

## Directory structure

```
tls/
├── ca.crt              # CA certificate (shared trust root)
├── server.crt          # Server certificate (Postgres, Redis, keyservice)
├── server.key          # Server private key
├── client.crt          # Client certificate (for mTLS)
├── client.key          # Client private key
└── README.md           # This file
```

## Quick start: generate self-signed certs for testing

```bash
# Generate a CA
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
  -keyout ca.key -out ca.crt \
  -subj "/CN=Mint Internal CA"

# Generate server cert signed by the CA (with SANs for Docker service names)
openssl req -newkey rsa:2048 -nodes -keyout server.key -out server.csr \
  -subj "/CN=mint-server"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 \
  -extfile <(printf "subjectAltName=DNS:postgres,DNS:redis,DNS:keyservice,DNS:nginx,DNS:localhost")

# Generate client cert for mTLS
openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr \
  -subj "/CN=mint-client"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 365

# Clean up CSR files
rm -f server.csr client.csr
```

## Production recommendations

1. **Use a proper PKI or secret manager** (HashiCorp Vault, AWS ACM Private CA,
   etc.) instead of self-signed certificates.
2. **Rotate certificates** before expiry. Automate rotation via cert-manager or
   Vault's PKI engine.
3. **Never commit private keys** to version control. Mount them as Docker secrets
   or volume mounts from a secret store.
4. **Set restrictive file permissions**: `chmod 600 *.key` and ensure only the
   service user can read them.

## Using the production overlay

```bash
# Generate certs first (see above), then:
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

This overlay:
- Enables `sslmode=verify-ca` on Postgres connections
- Switches Redis to TLS (`rediss://` scheme)
- Mounts CA certificates into service containers
