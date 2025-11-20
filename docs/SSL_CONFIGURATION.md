# SSL/TLS Configuration Guide

This guide explains how to configure SSL/TLS connections for Steep when connecting to PostgreSQL.

## SSL Modes

PostgreSQL supports six SSL modes, each providing different levels of security:

### disable
- No SSL/TLS encryption
- Data is transmitted in plaintext
- **Use only for local development or trusted networks**

```yaml
connection:
  sslmode: disable
```

### allow
- Try non-SSL connection first
- If server requires SSL, reconnect with SSL
- **Not recommended** - vulnerable to downgrade attacks

```yaml
connection:
  sslmode: allow
```

### prefer (default)
- Try SSL connection first
- Fall back to non-SSL if server doesn't support SSL
- Provides encryption when available without requiring it

```yaml
connection:
  sslmode: prefer
```

### require
- Require SSL connection
- Does **not** verify server certificate
- Protects against eavesdropping but **not** man-in-the-middle attacks

```yaml
connection:
  sslmode: require
```

### verify-ca
- Require SSL connection
- Verify server certificate is signed by a trusted CA
- Protects against man-in-the-middle attacks
- **Requires** `sslrootcert` configuration

```yaml
connection:
  sslmode: verify-ca
  sslrootcert: /path/to/ca-certificate.crt
```

### verify-full (most secure)
- Require SSL connection
- Verify server certificate is signed by a trusted CA
- Verify server hostname matches certificate
- **Recommended for production**
- **Requires** `sslrootcert` configuration

```yaml
connection:
  sslmode: verify-full
  sslrootcert: /path/to/ca-certificate.crt
```

## SSL Certificate Configuration

### Server CA Certificate (Root Certificate)

Required for `verify-ca` and `verify-full` modes.

```yaml
connection:
  sslmode: verify-full
  sslrootcert: /path/to/server-ca.crt
```

### Client Certificates (Mutual TLS)

Some PostgreSQL servers require client certificate authentication. Configure both certificate and key:

```yaml
connection:
  sslmode: verify-full
  sslrootcert: /path/to/server-ca.crt
  sslcert: /path/to/client.crt
  sslkey: /path/to/client.key
```

## Environment Variables

SSL settings can also be configured via environment variables:

```bash
export STEEP_CONNECTION_SSLMODE=verify-full
export STEEP_CONNECTION_SSLROOTCERT=/path/to/ca.crt
export STEEP_CONNECTION_SSLCERT=/path/to/client.crt
export STEEP_CONNECTION_SSLKEY=/path/to/client.key
```

Environment variables take precedence over config file settings.

## PostgreSQL Server Configuration

### Enabling SSL on PostgreSQL Server

1. Generate server certificates (or use existing CA-signed certificates)

2. Edit `postgresql.conf`:
```
ssl = on
ssl_cert_file = '/path/to/server.crt'
ssl_key_file = '/path/to/server.key'
ssl_ca_file = '/path/to/ca.crt'  # For client certificate verification
```

3. Edit `pg_hba.conf` to require SSL:
```
# Require SSL for all connections
hostssl all all 0.0.0.0/0 md5

# Require client certificate
hostssl all all 0.0.0.0/0 cert
```

4. Restart PostgreSQL

## Troubleshooting

### Certificate Validation Errors

If you see certificate validation errors with `verify-ca` or `verify-full`:

1. Verify the root certificate file exists and is readable
2. Ensure the certificate is in PEM format
3. Check that the server certificate is signed by the CA in your root certificate
4. For `verify-full`, ensure the server hostname matches the certificate CN or SAN

### Connection Refused with SSL

If SSL connections are refused:

1. Verify PostgreSQL server has `ssl = on` in `postgresql.conf`
2. Check that SSL certificate files exist and are readable by PostgreSQL
3. Verify firewall allows connections on the PostgreSQL port
4. Check PostgreSQL logs for SSL-related errors

### Certificate File Not Found

If you see "SSL certificate file not found" errors:

1. Use absolute paths for certificate files
2. Ensure the user running Steep has read permissions on the certificate files
3. Verify the paths are correct and files exist

## Security Best Practices

1. **Production**: Use `verify-full` mode with valid CA-signed certificates
2. **Staging**: Use at least `require` mode
3. **Development**: Use `prefer` or `disable` for local databases only
4. **Never** commit certificate files or private keys to version control
5. Set proper file permissions on key files (600 or 400)
6. Regularly rotate certificates before expiration
7. Use client certificates for additional authentication layer
8. Keep certificate authority (CA) certificates up to date

## Examples

### Local Development (Postgres.app)
```yaml
connection:
  host: localhost
  port: 5432
  database: mydb
  user: myuser
  sslmode: prefer  # Works with or without SSL
```

### Production (AWS RDS)
```yaml
connection:
  host: mydb.abc123.us-east-1.rds.amazonaws.com
  port: 5432
  database: production
  user: app_user
  sslmode: verify-full
  sslrootcert: /etc/steep/rds-ca-cert.pem
```

### Production with Client Certificates
```yaml
connection:
  host: secure-db.example.com
  port: 5432
  database: production
  user: app_user
  sslmode: verify-full
  sslrootcert: /etc/steep/ca.crt
  sslcert: /etc/steep/client.crt
  sslkey: /etc/steep/client.key
```

## Additional Resources

- [PostgreSQL SSL Documentation](https://www.postgresql.org/docs/current/ssl-tcp.html)
- [libpq Connection Parameters](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-PARAMKEYWORDS)
- [OpenSSL Certificate Management](https://www.openssl.org/docs/man1.1.1/man1/openssl-req.html)
